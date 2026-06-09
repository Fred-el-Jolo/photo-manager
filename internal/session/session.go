// Package session models the persistent state of one photo-organisation run.
package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

const sessionFileName = ".photo-manager-session.json"

// SessionPhoto represents a single photo and all user decisions about it.
type SessionPhoto struct {
	Path        string    `json:"path"`
	SHA256      string    `json:"sha256"`
	PHash       uint64    `json:"phash,omitempty"`
	TakenAt     time.Time `json:"taken_at,omitempty"`
	Width       int       `json:"width,omitempty"`
	Height      int       `json:"height,omitempty"`
	FileSize    int64     `json:"file_size,omitempty"`
	BlurScore   float64   `json:"blur_score,omitempty"`
	IsDuplicate bool      `json:"is_duplicate,omitempty"`
	IsRemoved   bool      `json:"is_removed,omitempty"`
	Rotation    int       `json:"rotation,omitempty"`
	NewName     string    `json:"new_name,omitempty"`
}

// Group is a named collection of photos that will be exported together.
type Group struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Photos          []SessionPhoto `json:"photos"`
	Applied         bool           `json:"applied,omitempty"`
	SuggestedKeeper string         `json:"suggested_keeper,omitempty"`
}

// MonthGroup is all groups for a given calendar month.
type MonthGroup struct {
	Year   int     `json:"year"`
	Month  int     `json:"month"`
	Groups []Group `json:"groups"`
}

// Session is the complete state of one photo-organisation run.
type Session struct {
	InputDir  string       `json:"input_dir"`
	OutputDir string       `json:"output_dir"`
	ScannedAt time.Time    `json:"scanned_at"`
	Months    []MonthGroup `json:"months"`
}

var idCounter atomic.Uint64

// NewGroupID returns a unique group ID without external dependencies.
func NewGroupID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), idCounter.Add(1))
}

// New returns an empty Session for the given directories.
func New(inputDir, outputDir string) *Session {
	return &Session{InputDir: inputDir, OutputDir: outputDir}
}

// Path returns the full path of the session file.
func (s *Session) Path() string {
	return filepath.Join(s.OutputDir, sessionFileName)
}

// Save writes the session atomically to <outputDir>/.photo-manager-session.json.
func (s *Session) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.Path()), 0755); err != nil {
		return fmt.Errorf("creating session directory: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling session: %w", err)
	}
	tmp := s.Path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("writing session tmp file: %w", err)
	}
	if err := os.Rename(tmp, s.Path()); err != nil {
		return fmt.Errorf("renaming session file: %w", err)
	}
	return nil
}

// Load reads the session file from <outputDir>/.photo-manager-session.json.
func Load(outputDir string) (*Session, error) {
	path := filepath.Join(outputDir, sessionFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no session at %s: %w", path, os.ErrNotExist)
		}
		return nil, fmt.Errorf("reading session file: %w", err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing session file: %w", err)
	}
	return &s, nil
}

// FindGroup returns a pointer to the Group with the given ID, or nil.
func (s *Session) FindGroup(id string) *Group {
	for i := range s.Months {
		for j := range s.Months[i].Groups {
			if s.Months[i].Groups[j].ID == id {
				return &s.Months[i].Groups[j]
			}
		}
	}
	return nil
}

// FindPhoto returns pointers to the SessionPhoto and its parent Group for the given path.
// Returns (nil, nil) if not found.
func (s *Session) FindPhoto(path string) (*SessionPhoto, *Group) {
	for i := range s.Months {
		for j := range s.Months[i].Groups {
			g := &s.Months[i].Groups[j]
			for k := range g.Photos {
				if g.Photos[k].Path == path {
					return &g.Photos[k], g
				}
			}
		}
	}
	return nil, nil
}

// MovePhoto removes the photo at photoPath from its current group and appends
// it to targetGroupID. Returns an error if either is not found.
func (s *Session) MovePhoto(photoPath, targetGroupID string) error {
	var srcGroup *Group
	var photoIdx int
	for i := range s.Months {
		for j := range s.Months[i].Groups {
			g := &s.Months[i].Groups[j]
			for k := range g.Photos {
				if g.Photos[k].Path == photoPath {
					srcGroup = g
					photoIdx = k
				}
			}
		}
	}
	if srcGroup == nil {
		return fmt.Errorf("photo not found: %s", photoPath)
	}
	dst := s.FindGroup(targetGroupID)
	if dst == nil {
		return fmt.Errorf("target group not found: %s", targetGroupID)
	}
	if srcGroup.ID == dst.ID {
		return nil
	}
	photo := srcGroup.Photos[photoIdx]
	srcGroup.Photos = append(srcGroup.Photos[:photoIdx], srcGroup.Photos[photoIdx+1:]...)
	dst.Photos = append(dst.Photos, photo)
	return nil
}

// CreateGroup ensures a group with the given (year, month, name) exists and
// returns it. If a matching group already exists it is returned unchanged.
func (s *Session) CreateGroup(year, month int, name string) *Group {
	for i := range s.Months {
		m := &s.Months[i]
		if m.Year != year || m.Month != month {
			continue
		}
		for j := range m.Groups {
			if m.Groups[j].Name == name {
				return &m.Groups[j]
			}
		}
		m.Groups = append(m.Groups, Group{ID: NewGroupID(), Name: name})
		return &m.Groups[len(m.Groups)-1]
	}
	s.Months = append(s.Months, MonthGroup{
		Year:   year,
		Month:  month,
		Groups: []Group{{ID: NewGroupID(), Name: name}},
	})
	sortMonths(s.Months)
	for i := range s.Months {
		if s.Months[i].Year == year && s.Months[i].Month == month {
			return &s.Months[i].Groups[len(s.Months[i].Groups)-1]
		}
	}
	return nil
}

func sortMonths(months []MonthGroup) {
	for i := 1; i < len(months); i++ {
		for j := i; j > 0; j-- {
			a, b := months[j-1], months[j]
			if a.Year > b.Year || (a.Year == b.Year && a.Month > b.Month) {
				months[j-1], months[j] = months[j], months[j-1]
			} else {
				break
			}
		}
	}
}
