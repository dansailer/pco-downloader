package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/dansailer/pco-downloader/internal/pco"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "pco-dl: %s\n", explain(err))
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("pco-dl", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outDir := fs.String("o", "", "directory for saved files (default: Downloads in your home directory)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `pco-dl lists the files on a Planning Center Services song or plan and downloads the ones you select.

Usage:
  pco-dl [flags] <song-or-plan>

Examples:
  pco-dl 31193598
  pco-dl https://services.planningcenteronline.com/songs/31193598
  pco-dl https://services.planningcenteronline.com/plans/89693797

A number is a song id. An arrangement URL lists every arrangement of that song.
A plan URL lists the arrangement selected on that plan, plus files stored on each song.
A song scheduled twice is listed once. Items with no song are skipped.

Authentication:
  Create a personal access token at
  https://api.planningcenteronline.com/oauth/applications
  Export it as PCO_APP_ID and PCO_SECRET, or let the command ask and save it
  in your config directory (mode 0600).

Files are saved in your Downloads folder as "<song>_<filename>".
Audio is saved as MP3, including files the song page only streams.
An MP3 stream is copied. Other audio and playlists are converted with ffmpeg.
Pass -o to choose a different directory.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("give one song URL, song id, or plan URL")
	}

	ref, err := pco.ParseRef(fs.Arg(0))
	if err != nil {
		return err
	}
	appID, secret, err := loadCredentials()
	if err != nil {
		return err
	}

	client := pco.New(appID, secret)
	title, id, detail, empty, files, songColumn, err := loadFiles(client, ref)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Printf("%s (%s)\n\n%s\n", title, id, empty)
		return nil
	}

	destRoot := *outDir
	if destRoot == "" {
		destRoot, err = userDownloads()
		if err != nil {
			return err
		}
	}

	if err := printFiles(os.Stdout, title, id, detail, files, songColumn); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "Saving into %s as \"<song>_<file>\". Audio is MP3, including stream-only files.\n", destRoot)
	fmt.Fprintln(os.Stderr, "ffmpeg converts a stream that is not already MP3.")
	fmt.Fprint(os.Stderr, "Select files (1,3-5 or all). Enter cancels: ")
	line, err := readLine(os.Stdin)
	if err != nil {
		return err
	}
	indexes, err := pco.ParseSelection(line, len(files))
	if errors.Is(err, pco.ErrCancel) {
		fmt.Println("Cancelled.")
		return nil
	}
	if err != nil {
		return err
	}

	var failed int
	var saved int
	for _, i := range indexes {
		f := files[i]
		if f.Access() == "link" {
			fmt.Printf("link only: %s\n  %s\n", f.Label(), f.Link)
			continue
		}
		dest, err := pco.DestPath(destRoot, songName(f, title), f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s\n", f.Label(), err)
			failed++
			continue
		}
		dest, err = pco.UniquePath(dest)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s\n", f.Label(), err)
			failed++
			continue
		}
		if err := client.Download(context.Background(), f, dest); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s\n", f.Label(), explain(err))
			failed++
			continue
		}
		fmt.Printf("saved %s\n", dest)
		saved++
	}
	if failed > 0 {
		return fmt.Errorf("saved %d, failed %d", saved, failed)
	}
	return nil
}

func userDownloads() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return downloadsDir(home)
}

// downloadsDir is $HOME/Downloads. A missing folder is an error. A symlink is
// accepted only when its target stays inside home.
func downloadsDir(home string) (string, error) {
	if home == "" {
		return "", errors.New("home directory is not set")
	}
	dir := filepath.Join(home, "Downloads")
	fi, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no Downloads folder at %s", dir)
		}
		return "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return "", err
		}
		homeResolved, err := filepath.EvalSymlinks(home)
		if err != nil {
			homeResolved = home
		}
		rel, err := filepath.Rel(homeResolved, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", fmt.Errorf("the Downloads folder at %s points outside your home directory", dir)
		}
		fi, err = os.Stat(resolved)
		if err != nil {
			return "", err
		}
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	return dir, nil
}

func loadFiles(client *pco.Client, ref pco.Ref) (title, id, detail, empty string, files []pco.File, songColumn bool, err error) {
	ctx := context.Background()
	switch ref.Kind {
	case pco.RefPlan:
		var plan pco.Plan
		plan, files, err = client.FilesForPlan(ctx, ref.ID)
		if err != nil {
			return "", "", "", "", nil, false, err
		}
		return plan.Title, plan.ID, plan.Dates, "No files are attached to the songs on this plan.", files, true, nil
	default:
		var song pco.Song
		song, files, err = client.FilesForSong(ctx, ref.ID)
		if err != nil {
			return "", "", "", "", nil, false, err
		}
		return song.Title, song.ID, song.Author, "No files are attached to this song or its arrangements.", files, false, nil
	}
}

func songName(f pco.File, fallback string) string {
	if f.SongTitle != "" {
		return f.SongTitle
	}
	return fallback
}

func printFiles(w io.Writer, title, id, detail string, files []pco.File, songColumn bool) error {
	if _, err := fmt.Fprintf(w, "%s (%s)\n", title, id); err != nil {
		return err
	}
	if detail != "" {
		if _, err := fmt.Fprintln(w, detail); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	header := "#\tAccess\tArrangement\tKey\tName\tSize"
	if songColumn {
		header = "#\tSong\tAccess\tArrangement\tKey\tName\tSize"
	}
	if _, err := fmt.Fprintln(tw, header); err != nil {
		return err
	}
	for i, f := range files {
		key := f.Key
		if key == "" {
			key = "—"
		}
		if songColumn {
			if _, err := fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n", i+1, f.SongTitle, f.Access(), f.Arrangement, key, f.Label(), formatSize(f.Bytes)); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", i+1, f.Access(), f.Arrangement, key, f.Label(), formatSize(f.Bytes)); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func formatSize(n int64) string {
	if n <= 0 {
		return "—"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div := int64(unit)
	exp := 0
	for n/div >= unit && exp < len("KMGT")-1 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

func explain(err error) string {
	var api *pco.APIError
	if errors.As(err, &api) && api.StatusCode == 401 {
		return "Planning Center rejected the application id or secret. Create a personal access token at https://api.planningcenteronline.com/oauth/applications"
	}
	return err.Error()
}

func readLine(r io.Reader) (string, error) {
	var buf []byte
	var b [1]byte
	for {
		n, err := r.Read(b[:])
		if n == 1 && b[0] != '\r' {
			if b[0] == '\n' {
				return string(buf), nil
			}
			buf = append(buf, b[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return string(buf), nil
			}
			return "", err
		}
	}
}
