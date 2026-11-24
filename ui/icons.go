package ui

import "github.com/egoist/mygo/ui"

// These line glyphs are shared by the surfaces that have not moved to SF
// Symbols yet (currently settings). They are parsed once and reused.
var (
	IconRefresh     = icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M20 11a8 8 0 0 0-14.3-4.9L3 9"/><path d="M3 4v5h5"/><path d="M4 13a8 8 0 0 0 14.3 4.9L21 15"/><path d="M21 20v-5h-5"/></svg>`)
	IconChevronDown = icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg>`)
	IconAlert       = icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10.3 4.3 2.6 18a2 2 0 0 0 1.7 3h15.4a2 2 0 0 0 1.7-3L13.7 4.3a2 2 0 0 0-3.4 0z"/><path d="M12 9v4"/><path d="M12 17h.01"/></svg>`)
	// IconFolderPlus is the plus menu's new-group entry: a folder with a plus.
	IconFolderPlus = icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6.5A1.5 1.5 0 0 1 4.5 5h4.1l2 2.2h8.9A1.5 1.5 0 0 1 21 8.7v9.8A1.5 1.5 0 0 1 19.5 20h-15A1.5 1.5 0 0 1 3 18.5z"/><path d="M12 11.2v5.6"/><path d="M9.2 14h5.6"/></svg>`)
	// IconPlayRectangle is the plus menu's add-subscription entry: a screen.
	IconPlayRectangle = icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="5" width="18" height="14" rx="3"/><path d="m10.4 9.3 4.8 2.7-4.8 2.7z"/></svg>`)
	// IconImport is an arrow pointing down into a tray.
	IconImport = icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/><polyline points="7 11 12 16 17 11"/><line x1="12" y1="4" x2="12" y2="16"/></svg>`)
	// IconExport is an arrow pointing up out of a tray.
	IconExport = icon(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="4" x2="12" y2="15"/></svg>`)
)

func icon(svg string) *ui.SVG { return ui.MustParseSVG([]byte(svg)) }
