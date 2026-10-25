package app

import "sync"

// previewRenderCache remembers the last drawn preview.
//
// Drawing a preview means measuring and clipping lines that carry ANSI
// colour codes, and measuring a styled string is not cheap: the width of
// the visible text has to be worked out by parsing the escapes. The preview
// does not change between frames — only when the cursor moves or the
// terminal is resized — so redrawing it every frame is work whose result
// is thrown away.
//
// It is reached through a pointer because Model is copied by value on every
// update, and a cache that lived in the copy would never be hit.
type previewRenderCache struct {
	mu       sync.Mutex
	rendered string
	// path is the file the drawn preview belongs to.
	path string
	// version counts the changes to the preview, so a changed preview is
	// not served from the cache.
	version int
	// width and height are the panel dimensions the preview was drawn at.
	width  int
	height int
}

// get returns the drawn preview if it still matches what is being asked
// for.
func (c *previewRenderCache) get(path string, version, width, height int) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rendered == "" {
		return "", false
	}
	if c.path != path || c.version != version || c.width != width || c.height != height {
		return "", false
	}
	return c.rendered, true
}

// put records a drawn preview.
func (c *previewRenderCache) put(path string, version, width, height int, rendered string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rendered = rendered
	c.path = path
	c.version = version
	c.width = width
	c.height = height
}
