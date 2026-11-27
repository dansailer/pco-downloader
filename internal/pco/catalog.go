package pco

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Song is the Services song the files belong to.
type Song struct {
	ID     string
	Title  string
	Author string
}

// File is one attachment on a song, arrangement, or key.
type File struct {
	ID               string
	SongTitle        string
	Arrangement      string
	Key              string
	DisplayName      string
	Filename         string
	ContentType      string
	Bytes            int64
	AllowMP3Download bool
	Link             string
	URL              string
}

// Label is the name shown in the picker.
func (f File) Label() string {
	switch {
	case f.DisplayName != "" && f.Filename != "" && f.DisplayName != f.Filename:
		return f.DisplayName + " (" + f.Filename + ")"
	case f.DisplayName != "":
		return f.DisplayName
	case f.Filename != "":
		return f.Filename
	case f.Link != "":
		return f.Link
	default:
		return "attachment " + f.ID
	}
}

// Access describes how Planning Center exposes the attachment.
// "link" is a remote URL with no stored file.
// "youtube" is a YouTube link whose audio can be saved as MP3.
// "stream-only" is audio whose song-page download is turned off.
// "download" is anything else this command will request through the API.
func (f File) Access() string {
	if _, ok := youtubeTarget(f); ok {
		return "youtube"
	}
	if f.Filename == "" && f.Link != "" {
		return "link"
	}
	if isAudio(f.ContentType, f.Filename) && !f.AllowMP3Download {
		return "stream-only"
	}
	return "download"
}

func isAudio(contentType, filename string) bool {
	if strings.HasPrefix(strings.ToLower(contentType), "audio/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".mp3", ".m4a", ".wav", ".aac", ".flac", ".ogg", ".aiff", ".aif":
		return true
	default:
		return false
	}
}

type songAttrs struct {
	Title  string `json:"title"`
	Author string `json:"author"`
}

type arrangementAttrs struct {
	Name string `json:"name"`
}

type keyAttrs struct {
	Name        string `json:"name"`
	StartingKey string `json:"starting_key"`
}

type attachmentAttrs struct {
	DisplayName      string `json:"display_name"`
	Filename         string `json:"filename"`
	ContentType      string `json:"content_type"`
	FileSize         int64  `json:"file_size"`
	AllowMP3Download bool   `json:"allow_mp3_download"`
	LinkedURL        string `json:"linked_url"`
	RemoteLink       string `json:"remote_link"`
	URL              string `json:"url"`
}

func (c *Client) songByID(ctx context.Context, songID string) (Song, error) {
	res, err := c.getResource(ctx, "/services/v2/songs/"+url.PathEscape(songID))
	if err != nil {
		return Song{}, err
	}
	attrs, err := decodeAttrs[songAttrs](res.Attributes)
	if err != nil {
		return Song{}, fmt.Errorf("decode song: %w", err)
	}
	song := Song{ID: res.ID, Title: attrs.Title, Author: attrs.Author}
	if song.Title == "" {
		song.Title = "Song " + song.ID
	}
	return song, nil
}

// FilesForSong loads the song and every attachment on its arrangements and keys.
// Attachments stored on the song itself are included under the arrangement name "Song".
func (c *Client) FilesForSong(ctx context.Context, songID string) (Song, []File, error) {
	song, err := c.songByID(ctx, songID)
	if err != nil {
		return Song{}, nil, err
	}

	arrs, err := c.list(ctx, "/services/v2/songs/"+url.PathEscape(songID)+"/arrangements")
	if err != nil {
		return Song{}, nil, err
	}

	var files []File
	seen := map[string]bool{}
	add := func(f File) {
		if f.ID == "" || seen[f.ID] {
			return
		}
		seen[f.ID] = true
		files = append(files, f)
	}

	songFiles, err := c.attachments(ctx, "/services/v2/songs/"+url.PathEscape(songID)+"/attachments", "Song", "")
	if err != nil {
		var api *APIError
		if errors.As(err, &api) && api.StatusCode == http.StatusNotFound {
			songFiles = nil
		} else {
			return Song{}, nil, err
		}
	}

	var arranged []File
	for _, arr := range arrs {
		aAttrs, err := decodeAttrs[arrangementAttrs](arr.Attributes)
		if err != nil {
			return Song{}, nil, fmt.Errorf("decode arrangement %s: %w", arr.ID, err)
		}
		name := aAttrs.Name
		if name == "" {
			name = "Arrangement " + arr.ID
		}
		base := "/services/v2/songs/" + url.PathEscape(songID) + "/arrangements/" + url.PathEscape(arr.ID)
		onArr, err := c.attachments(ctx, base+"/attachments", name, "")
		if err != nil {
			return Song{}, nil, err
		}
		arranged = append(arranged, onArr...)

		keys, err := c.list(ctx, base+"/keys")
		if err != nil {
			return Song{}, nil, err
		}
		for _, key := range keys {
			kAttrs, err := decodeAttrs[keyAttrs](key.Attributes)
			if err != nil {
				return Song{}, nil, fmt.Errorf("decode key %s: %w", key.ID, err)
			}
			onKey, err := c.attachments(ctx, base+"/keys/"+url.PathEscape(key.ID)+"/attachments", name, keyLabel(kAttrs))
			if err != nil {
				return Song{}, nil, err
			}
			arranged = append(arranged, onKey...)
		}
	}

	for _, f := range arranged {
		add(f)
	}
	var head []File
	for _, f := range songFiles {
		if f.ID != "" && !seen[f.ID] {
			seen[f.ID] = true
			head = append(head, f)
		}
	}
	out := append(head, files...)
	for i := range out {
		out[i].SongTitle = song.Title
	}
	return song, out, nil
}

func (c *Client) attachments(ctx context.Context, path, arrangement, key string) ([]File, error) {
	rows, err := c.list(ctx, path)
	if err != nil {
		return nil, err
	}
	out := make([]File, 0, len(rows))
	for _, row := range rows {
		attrs, err := decodeAttrs[attachmentAttrs](row.Attributes)
		if err != nil {
			return nil, fmt.Errorf("decode attachment %s: %w", row.ID, err)
		}
		link := attrs.RemoteLink
		if link == "" {
			link = attrs.LinkedURL
		}
		out = append(out, File{
			ID:               row.ID,
			Arrangement:      arrangement,
			Key:              key,
			DisplayName:      attrs.DisplayName,
			Filename:         attrs.Filename,
			ContentType:      attrs.ContentType,
			Bytes:            attrs.FileSize,
			AllowMP3Download: attrs.AllowMP3Download,
			Link:             link,
			URL:              attrs.URL,
		})
	}
	return out, nil
}

func keyLabel(attrs keyAttrs) string {
	name := strings.TrimSpace(attrs.Name)
	start := strings.TrimSpace(attrs.StartingKey)
	switch {
	case start == "":
		return name
	case name == "" || name == "Default" || strings.EqualFold(name, start):
		return start
	default:
		return start + " · " + name
	}
}

// Download saves one stored attachment.
// Audio, including stream-only files, is written as an MP3. The playback URL
// comes from the open action. When that action is refused, the attachment URL
// returned with the file is used instead. MP3 bytes are copied. Other audio
// and playlists are converted with ffmpeg.
func (c *Client) Download(ctx context.Context, f File, dest string) error {
	if page, ok := youtubeTarget(f); ok {
		return c.saveYouTube(ctx, page, dest)
	}
	if f.Access() == "link" {
		return fmt.Errorf("%s is a link, not a stored file", f.Label())
	}
	fileURL, openErr := c.openAttachment(ctx, f.ID)
	if openErr != nil {
		if err := c.validateFetchURL(f.URL); err != nil {
			return openErr
		}
		fileURL = f.URL
	}
	err := c.saveFetched(ctx, f, fileURL, dest)
	if err != nil && f.URL != "" && f.URL != fileURL && c.validateFetchURL(f.URL) == nil {
		if err2 := c.saveFetched(ctx, f, f.URL, dest); err2 == nil {
			return nil
		} else {
			err = fmt.Errorf("%w; attachment url: %w", err, err2)
		}
	}
	if err != nil && openErr != nil && fileURL == f.URL {
		return fmt.Errorf("%w (open failed: %w)", err, openErr)
	}
	return err
}

func (c *Client) saveFetched(ctx context.Context, f File, fileURL, dest string) error {
	if id, ok := youtubeVideoID(fileURL); ok && !storedMedia(f.Filename) {
		return c.saveYouTube(ctx, youtubePage(id), dest)
	}
	if isAudio(f.ContentType, f.Filename) {
		return c.saveAudio(ctx, fileURL, dest)
	}
	return c.saveRaw(ctx, fileURL, dest)
}

func saveStream(dest string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	if err := refuseSymlink(dest); err != nil {
		return err
	}
	tmp := dest + ".part"
	out, err := createExclusive(tmp, 0o644)
	if err != nil {
		return err
	}
	copyErr := copyLimited(out, r)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := refuseSymlink(dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// createExclusive creates path without following a symlink. An existing
// regular file is replaced; anything else is refused.
func createExclusive(path string, mode os.FileMode) (*os.File, error) {
	fi, err := os.Lstat(path)
	if err == nil {
		if !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode) //nolint:gosec // G304: path is the destination .part file, created with O_EXCL after the symlink check
}

func refuseSymlink(path string) error {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink", path)
	}
	return nil
}

func copyLimited(dst io.Writer, src io.Reader) error {
	n, err := io.Copy(dst, io.LimitReader(src, maxDownload+1))
	if n > maxDownload {
		return fmt.Errorf("file is larger than %d MB", maxDownload>>20)
	}
	return err
}
