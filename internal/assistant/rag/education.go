// Education text source for retrieval: installed HTML packs are read through
// the education service. PDF packs are excluded (no parser is introduced)
// and ZIM packs are already covered by Kiwix search. Only installed HTML
// resources participate, and only their validated local routes are exposed.
package rag

import (
	"io"
	"sort"

	"engelsiz/internal/download"
	"engelsiz/internal/education"
	"engelsiz/internal/packages"
)

// TextRef is one installed education HTML pack.
type TextRef struct {
	ID    string
	Title string
	Href  string
}

// TextIndex abstracts installed text packs for retrieval tests.
type TextIndex interface {
	InstalledTexts() ([]TextRef, error)
	OpenText(id string) (io.ReadCloser, error)
}

// EducationTexts adapts *education.Service to TextIndex. Manager and Pkgs
// are passed through to List; a nil Service disables the source.
type EducationTexts struct {
	Service  *education.Service
	Manager  *download.Manager
	Packages *packages.Service
}

// InstalledTexts lists installed HTML packs in stable id order.
func (e *EducationTexts) InstalledTexts() ([]TextRef, error) {
	if e == nil || e.Service == nil {
		return nil, nil
	}
	res, err := e.Service.List(e.Manager, e.Packages)
	if err != nil {
		return nil, err
	}
	var out []TextRef
	for _, v := range res.Resources {
		if v.Format != education.FormatHTML || v.Status != education.StatusInstalled {
			continue
		}
		out = append(out, TextRef{ID: v.ID, Title: v.Title, Href: "/education/content/" + v.ID})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// OpenText opens one installed HTML pack payload for a bounded read.
func (e *EducationTexts) OpenText(id string) (io.ReadCloser, error) {
	if e == nil || e.Service == nil {
		return nil, io.ErrUnexpectedEOF
	}
	f, _, _, err := e.Service.Open(id)
	if err != nil {
		return nil, err
	}
	return f, nil
}
