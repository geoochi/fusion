package handler

import (
	"encoding/xml"
	"net/http"
	"time"

	"github.com/0x2E/fusion/internal/model"
	"github.com/gin-gonic/gin"
)

// OPML document structures. encoding/xml handles attribute escaping safely.
type opmlDocument struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    opmlHead `xml:"head"`
	Body    opmlBody `xml:"body"`
}

type opmlHead struct {
	Title       string `xml:"title"`
	DateCreated string `xml:"dateCreated,omitempty"`
}

type opmlBody struct {
	Outlines []opmlOutline `xml:"outline"`
}

type opmlOutline struct {
	Type     string        `xml:"type,attr,omitempty"`
	Text     string        `xml:"text,attr"`
	Title    string        `xml:"title,attr"`
	XMLURL   string        `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string        `xml:"htmlUrl,attr,omitempty"`
	Outlines []opmlOutline `xml:"outline,omitempty"`
}

// exportOPML builds an OPML 2.0 document from the user's groups and feeds and
// returns it as a downloadable attachment. The grouped structure mirrors the
// frontend's generateOPML so exports are interchangeable.
func (h *Handler) exportOPML(c *gin.Context) {
	groups, err := h.store.ListGroups()
	if err != nil {
		internalError(c, err, "list groups for opml export")
		return
	}

	feeds, err := h.store.ListFeeds()
	if err != nil {
		internalError(c, err, "list feeds for opml export")
		return
	}

	doc := buildOPML(groups, feeds, time.Now().UTC())

	c.Header("Content-Disposition", `attachment; filename="fusion-subscriptions.opml"`)
	c.Data(http.StatusOK, "text/x-opml; charset=utf-8", doc)
}

// buildOPML constructs the OPML XML bytes from groups and feeds. Feeds are
// nested under their owning group; feeds whose group no longer exists are
// emitted at the top level of the body.
func buildOPML(groups []*model.Group, feeds []*model.Feed, now time.Time) []byte {
	groupByID := make(map[int64]int, len(groups))
	for i, g := range groups {
		groupByID[g.ID] = i
	}

	grouped := make([][]opmlOutline, len(groups))
	var ungrouped []opmlOutline

	for _, f := range feeds {
		outline := opmlOutline{
			Type:    "rss",
			Text:    f.Name,
			Title:   f.Name,
			XMLURL:  f.Link,
			HTMLURL: f.SiteURL,
		}
		if idx, ok := groupByID[f.GroupID]; ok {
			grouped[idx] = append(grouped[idx], outline)
		} else {
			ungrouped = append(ungrouped, outline)
		}
	}

	body := opmlBody{}
	for i, g := range groups {
		if len(grouped[i]) == 0 {
			continue
		}
		body.Outlines = append(body.Outlines, opmlOutline{
			Text:     g.Name,
			Title:    g.Name,
			Outlines: grouped[i],
		})
	}
	body.Outlines = append(body.Outlines, ungrouped...)

	doc := opmlDocument{
		Version: "2.0",
		Head: opmlHead{
			Title:       "Fusion Subscriptions",
			DateCreated: now.Format(time.RFC1123Z),
		},
		Body: body,
	}

	// MarshalIndent produces readable output; the leading XML declaration is
	// prepended because encoding/xml's Marshal does not emit one.
	data, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		// Marshalling only fails on unsupported types; the structs above are safe.
		return []byte(xml.Header + "<opml version=\"2.0\"><head><title>Fusion Subscriptions</title></head><body/></opml>")
	}
	return append([]byte(xml.Header), data...)
}
