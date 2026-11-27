package pco

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Plan is one service plan. Its files come from the songs scheduled on it.
type Plan struct {
	ID    string
	Title string
	Dates string
}

type planAttrs struct {
	Title       string `json:"title"`
	Dates       string `json:"dates"`
	SeriesTitle string `json:"series_title"`
}

// FilesForPlan lists files for the songs on one plan.
// The arrangement and key selected on the plan are included, plus files stored
// on the song itself. Other arrangements of those songs are left out. A song
// scheduled twice contributes its files once. Plan items with no song are skipped.
func (c *Client) FilesForPlan(ctx context.Context, planID string) (Plan, []File, error) {
	if !isPositiveID(planID) {
		return Plan{}, nil, fmt.Errorf("plan id %q is not valid", planID)
	}
	serviceTypeID, planRes, err := c.findPlan(ctx, planID)
	if err != nil {
		return Plan{}, nil, err
	}
	attrs, err := decodeAttrs[planAttrs](planRes.Attributes)
	if err != nil {
		return Plan{}, nil, fmt.Errorf("decode plan: %w", err)
	}
	plan := Plan{
		ID:    planRes.ID,
		Title: planHeading(attrs, planID),
		Dates: strings.TrimSpace(attrs.Dates),
	}
	if plan.Dates == plan.Title {
		plan.Dates = ""
	}

	items, err := c.list(ctx, "/services/v2/service_types/"+url.PathEscape(serviceTypeID)+"/plans/"+url.PathEscape(planID)+"/items")
	if err != nil {
		return Plan{}, nil, err
	}

	var files []File
	seen := map[string]bool{}
	loadedSong := map[string]bool{}
	arrNames := map[string]string{}
	keyNames := map[string]string{}
	for _, item := range items {
		songID, ok := item.toOne("song")
		if !ok {
			continue
		}
		song, err := c.songByID(ctx, songID)
		if err != nil {
			return Plan{}, nil, err
		}
		var songFiles []File
		if !loadedSong[song.ID] {
			loadedSong[song.ID] = true
			songFiles, err = c.attachments(ctx, "/services/v2/songs/"+url.PathEscape(song.ID)+"/attachments", "Song", "")
			if err != nil {
				var api *APIError
				if errors.As(err, &api) && api.StatusCode == http.StatusNotFound {
					songFiles = nil
				} else {
					return Plan{}, nil, err
				}
			}
		}

		var arranged []File
		if arrID, ok := item.toOne("arrangement"); ok {
			name, err := c.cachedArrangement(ctx, arrNames, song.ID, arrID)
			if err != nil {
				return Plan{}, nil, err
			}
			base := "/services/v2/songs/" + url.PathEscape(song.ID) + "/arrangements/" + url.PathEscape(arrID)
			onArr, err := c.attachments(ctx, base+"/attachments", name, "")
			if err != nil {
				return Plan{}, nil, err
			}
			arranged = append(arranged, onArr...)
			if keyID, ok := item.toOne("key"); ok {
				label, err := c.cachedKey(ctx, keyNames, song.ID, arrID, keyID)
				if err != nil {
					return Plan{}, nil, err
				}
				onKey, err := c.attachments(ctx, base+"/keys/"+url.PathEscape(keyID)+"/attachments", name, label)
				if err != nil {
					return Plan{}, nil, err
				}
				arranged = append(arranged, onKey...)
			}
		}
		files = append(files, newPlanFiles(seen, songFiles, arranged, song.Title)...)
	}
	return plan, files, nil
}

// findPlan opens the plan under the first service type this token can read.
// The public plan URL does not include the service type id.
func (c *Client) findPlan(ctx context.Context, planID string) (string, resource, error) {
	types, err := c.list(ctx, "/services/v2/service_types")
	if err != nil {
		return "", resource{}, err
	}
	if len(types) == 0 {
		return "", resource{}, errors.New("this token cannot see any service types")
	}
	for _, st := range types {
		if st.ID == "" {
			continue
		}
		path := "/services/v2/service_types/" + url.PathEscape(st.ID) + "/plans/" + url.PathEscape(planID)
		got, err := c.getResource(ctx, path)
		if err == nil {
			if got.ID == "" {
				got.ID = planID
			}
			return st.ID, got, nil
		}
		var api *APIError
		if errors.As(err, &api) && (api.StatusCode == http.StatusNotFound || api.StatusCode == http.StatusForbidden) {
			continue
		}
		return "", resource{}, err
	}
	return "", resource{}, fmt.Errorf("plan %s is not in a service type this token can open", planID)
}

func (c *Client) cachedArrangement(ctx context.Context, cache map[string]string, songID, arrID string) (string, error) {
	key := songID + "/" + arrID
	if name, ok := cache[key]; ok {
		return name, nil
	}
	res, err := c.getResource(ctx, "/services/v2/songs/"+url.PathEscape(songID)+"/arrangements/"+url.PathEscape(arrID))
	if err != nil {
		return "", err
	}
	attrs, err := decodeAttrs[arrangementAttrs](res.Attributes)
	if err != nil {
		return "", fmt.Errorf("decode arrangement %s: %w", arrID, err)
	}
	name := attrs.Name
	if name == "" {
		name = "Arrangement " + arrID
	}
	cache[key] = name
	return name, nil
}

func (c *Client) cachedKey(ctx context.Context, cache map[string]string, songID, arrID, keyID string) (string, error) {
	key := songID + "/" + arrID + "/" + keyID
	if label, ok := cache[key]; ok {
		return label, nil
	}
	res, err := c.getResource(ctx, "/services/v2/songs/"+url.PathEscape(songID)+"/arrangements/"+url.PathEscape(arrID)+"/keys/"+url.PathEscape(keyID))
	if err != nil {
		return "", err
	}
	attrs, err := decodeAttrs[keyAttrs](res.Attributes)
	if err != nil {
		return "", fmt.Errorf("decode key %s: %w", keyID, err)
	}
	label := keyLabel(attrs)
	if label == "" {
		label = "Key " + keyID
	}
	cache[key] = label
	return label, nil
}

func planHeading(attrs planAttrs, id string) string {
	for _, s := range []string{attrs.Title, attrs.SeriesTitle, attrs.Dates} {
		if t := strings.TrimSpace(s); t != "" {
			return t
		}
	}
	return "Plan " + id
}

// newPlanFiles returns files not already listed. A file that is both on the
// song and on the selected arrangement keeps the arrangement copy.
func newPlanFiles(seen map[string]bool, songFiles, arranged []File, songTitle string) []File {
	onArrangement := map[string]bool{}
	for _, f := range arranged {
		if f.ID != "" {
			onArrangement[f.ID] = true
		}
	}
	var out []File
	for _, f := range songFiles {
		if f.ID == "" || seen[f.ID] || onArrangement[f.ID] {
			continue
		}
		seen[f.ID] = true
		f.SongTitle = songTitle
		out = append(out, f)
	}
	for _, f := range arranged {
		if f.ID == "" || seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		f.SongTitle = songTitle
		out = append(out, f)
	}
	return out
}
