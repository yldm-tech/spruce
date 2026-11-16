// The paths are those of the React app (src/components/common/pages.tsx, CategoryIcon.tsx, maintenance/TaskIcon.tsx and the inline glyphs of the pages); keep them in step with the design.

package icons

// glyphs are the app's line icons on the 24-unit grid, by name: page/<route>, category/<Category.Icon key>, task/<maintenance.Icon> and the shared ones.
var glyphs = map[string]glyph{
	"page/overview":      {round: true, paths: `<circle cx="12" cy="12" r="8"/> <path d="M12 4v4M12 16v4M4 12h4M16 12h4"/>`},
	"page/system-junk":   {round: true, paths: `<path d="M14 3l-3 8"/> <path d="M5 12h12l1.5 8h-15z"/>`},
	"page/dev-junk":      {round: true, paths: `<path d="M8 7l-5 5 5 5M16 7l5 5-5 5"/>`},
	"page/installers":    {round: true, paths: `<path d="M12 4v11M7 10l5 5 5-5M5 20h14"/>`},
	"page/uninstall":     {round: true, paths: `<rect x="4" y="4" width="7" height="7" rx="2"/> <rect x="13" y="4" width="7" height="7" rx="2"/> <rect x="4" y="13" width="7" height="7" rx="2"/> <path d="M14 16.5h5"/>`},
	"page/space":         {round: true, paths: `<path d="M12 3a9 9 0 1 0 9 9h-9z"/>`},
	"page/maintenance":   {round: true, paths: `<path d="M13 3L5 14h6l-1 7 8-11h-6z"/>`},
	"page/health":        {round: true, paths: `<path d="M3 12h4l3-7 4 14 3-7h4"/>`},
	"page/history":       {round: true, paths: `<path d="M3.5 12a8.5 8.5 0 1 0 2.5-6"/> <path d="M3 4v4h4"/> <path d="M12 8v4l3 2"/>`},
	"page/settings":      {round: true, paths: `<path d="M4 7h9M17 7h3M4 17h3M11 17h9"/> <circle cx="15" cy="7" r="2"/> <circle cx="9" cy="17" r="2"/>`},
	"category/cache":     {round: true, paths: `<ellipse cx="12" cy="6" rx="7" ry="3"/> <path d="M5 6v6c0 1.7 3.1 3 7 3s7-1.3 7-3V6M5 12v6c0 1.7 3.1 3 7 3s7-1.3 7-3v-6"/>`},
	"category/log":       {round: true, paths: `<path d="M6 3h9l4 4v14H6z"/> <path d="M9 11h7M9 15h7M9 7h3"/>`},
	"category/crash":     {round: true, paths: `<path d="M12 4l9 16H3z"/> <path d="M12 10v4M12 17v.5"/>`},
	"category/temp":      {round: true, paths: `<circle cx="12" cy="13" r="7"/> <path d="M12 9v4l2.5 2M10 3h4"/>`},
	"category/trash":     {round: true, paths: `<path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13"/>`},
	"category/browser":   {round: true, paths: `<circle cx="12" cy="12" r="9"/> <path d="M3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18"/>`},
	"category/mail":      {round: true, paths: `<rect x="3" y="5" width="18" height="14" rx="2"/> <path d="M3 7l9 6 9-6"/>`},
	"category/update":    {round: true, paths: `<path d="M19.5 9A8 8 0 0 0 5 7.5L4 9"/> <path d="M4 4v5h5"/> <path d="M4.5 15A8 8 0 0 0 19 16.5l1-1.5"/> <path d="M20 20v-5h-5"/>`},
	"category/xcode":     {round: true, paths: `<path d="M15 4a5 5 0 0 0-5.6 6.6L4 16v4h4l5.4-5.4A5 5 0 0 0 20 9l-3 3-3-1-1-3z"/>`},
	"category/simulator": {round: true, paths: `<rect x="7" y="2" width="10" height="20" rx="2.5"/> <path d="M11 18h2"/>`},
	"category/node":      {round: true, paths: `<path d="M12 2l9 5v10l-9 5-9-5V7z"/> <path d="M12 22V12M21 7l-9 5-9-5"/>`},
	"category/package":   {round: true, paths: `<path d="M3 7l9-4 9 4v10l-9 4-9-4z"/> <path d="M3 7l9 4 9-4M12 11v10"/>`},
	"category/docker":    {round: true, paths: `<rect x="3" y="10" width="18" height="8" rx="2"/> <path d="M6 10V7h4v3M10 10V5h4v5M14 10V7h4v3"/>`},
	"category/build":     {round: true, paths: `<path d="M8 7l-5 5 5 5M16 7l5 5-5 5"/>`},
	"category/installer": {round: true, paths: `<path d="M12 4v11M7 10l5 5 5-5M5 20h14"/>`},
	"category/disk":      {round: true, paths: `<path d="M5 13l2.5-8h9L19 13"/> <rect x="3" y="13" width="18" height="7" rx="2"/> <path d="M7 16.5h.01M11 16.5h6"/>`},
	"category/app":       {round: true, paths: `<rect x="4" y="4" width="16" height="16" rx="4"/> <path d="M9 9h6v6H9z"/>`},
	"category/leftover":  {round: true, paths: `<rect x="4" y="4" width="7" height="7" rx="2"/> <rect x="13" y="4" width="7" height="7" rx="2"/> <rect x="4" y="13" width="7" height="7" rx="2"/> <path d="M14 16.5h5"/>`},
	"category/file":      {round: true, paths: `<path d="M6 3h9l4 4v14H6z"/> <path d="M15 3v4h4"/>`},
	"category/video":     {round: true, paths: `<rect x="3" y="6" width="13" height="12" rx="2"/> <path d="M16 10l5-3v10l-5-3"/>`},
	"category/archive":   {round: true, paths: `<rect x="3" y="4" width="18" height="5" rx="1.5"/> <path d="M5 9v10h14V9M10 13h4"/>`},
	"category/folder":    {round: true, paths: `<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>`},
	"task/network":       {round: false, paths: `<circle cx="12" cy="12" r="9"/> <path d="M3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18"/>`},
	"task/search":        {round: false, paths: `<circle cx="11" cy="11" r="7"/> <path d="M20 20l-4-4"/>`},
	"task/preview":       {round: false, paths: `<path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12z"/> <circle cx="12" cy="12" r="3"/>`},
	"task/icons":         {round: false, paths: `<rect x="4" y="4" width="16" height="16" rx="4"/> <path d="M9 9h6v6H9z"/>`},
	"task/open-with":     {round: true, paths: `<path d="M4 12h16M14 6l6 6-6 6"/>`},
	"task/database":      {round: false, paths: `<ellipse cx="12" cy="6" rx="7" ry="3"/> <path d="M5 6v12c0 1.7 3.1 3 7 3s7-1.3 7-3V6M5 12c0 1.7 3.1 3 7 3s7-1.3 7-3"/>`},
	"task/cleanup":       {round: true, paths: `<path d="M14 3l-3 8"/> <path d="M5 12h12l1.5 8h-15z"/>`},
	"task/repair":        {round: true, paths: `<path d="M15 4a5 5 0 0 0-5.6 6.6L4 16v4h4l5.4-5.4A5 5 0 0 0 20 9l-3 3-3-1-1-3z"/>`},
	"task/disk":          {round: true, paths: `<path d="M5 13l2.5-8h9L19 13"/> <rect x="3" y="13" width="18" height="7" rx="2"/> <path d="M7 16.5h.01M11 16.5h6"/>`},
	"task/update":        {round: true, paths: `<path d="M19.5 9A8 8 0 0 0 5 7.5L4 9"/> <path d="M4 4v5h5"/> <path d="M4.5 15A8 8 0 0 0 19 16.5l1-1.5"/> <path d="M20 20v-5h-5"/>`},
	"task/font":          {round: true, paths: `<path d="M5 20l7-16 7 16M8 14h8"/>`},
	"task/store":         {round: true, paths: `<path d="M5 8h14l-1 12H6z"/> <path d="M9 8V6a3 3 0 0 1 6 0v2"/>`},
	"task/network-fix":   {round: true, paths: `<path d="M9 3v5M15 3v5"/> <path d="M6 8h12v3a6 6 0 0 1-12 0z"/> <path d="M12 17v4"/>`},
	// Done badge, checkbox mark.
	"check": {round: true, paths: `<path d="M5 12.5l4.5 4.5L19 7.5"/>`},
	// Done badge of a run that did not complete.
	"exclaim": {round: true, paths: `<path d="M12 6.5v7M12 17.5v.5"/>`},
	// Indeterminate checkbox.
	"dash": {round: true, paths: `<path d="M6 12h12"/>`},
	// CPU alert banner.
	"alert": {round: true, paths: `<path d="M12 4l9 16H3z"/> <path d="M12 10v4M12 17v.5"/>`},
	// Error empty state.
	"error": {round: true, paths: `<path d="M12 7v6M12 17v.5"/>`},
	// Remove from a list.
	"close": {round: true, paths: `<path d="M6 6l12 12M18 6L6 18"/>`},
	// Add to a list.
	"plus": {round: true, paths: `<path d="M12 5v14M5 12h14"/>`},
	// Collapsed section.
	"chevron-down": {round: true, paths: `<path d="M6 9l6 6 6-6"/>`},
	// Expanded category.
	"chevron-up": {round: true, paths: `<path d="M6 15l6-6 6 6"/>`},
	// Collapsed row.
	"chevron-right": {round: true, paths: `<path d="M9 6l6 6-6 6"/>`},
	// Put back where it was (放回原处).
	"undo": {round: true, paths: `<path d="M3.5 12a8.5 8.5 0 1 0 2.5-6"/> <path d="M3 4v4h4"/>`},
	// Show in Finder or Explorer: a folder with an arrow out of it, not a magnifier, which reads as search.
	"reveal": {round: true, paths: `<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/> <path d="M9.5 15.5l5-5M10.5 10.5h4v4"/>`},
	// Move to the Trash.
	"trash": {round: true, paths: `<path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13"/>`},
	// A folder row.
	"folder": {round: true, paths: `<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>`},
	// A file row.
	"file": {round: true, paths: `<path d="M6 3h8l4 4v14H6z"/> <path d="M14 3v4h4"/>`},
	// A folded group row.
	"group": {round: true, paths: `<circle cx="6" cy="12" r="1.2"/> <circle cx="12" cy="12" r="1.2"/> <circle cx="18" cy="12" r="1.2"/>`},
	// Permissions.
	"lock": {round: true, paths: `<rect x="5" y="11" width="14" height="9" rx="2"/> <path d="M8 11V8a4 4 0 0 1 8 0v3"/>`},
	// About.
	"info": {round: true, paths: `<circle cx="12" cy="12" r="9"/> <path d="M12 11v5M12 8h.01"/>`},
	// CPU card.
	"cpu": {round: true, paths: `<rect x="6" y="6" width="12" height="12" rx="2"/> <path d="M9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4"/>`},
	// Memory card.
	"memory": {round: true, paths: `<rect x="3" y="6" width="18" height="10" rx="2"/> <path d="M7 16v3M11 16v3M15 16v3M7 10h2M11 10h2M15 10h2"/>`},
	// Disk card.
	"disk": {round: true, paths: `<path d="M5 13l2.5-8h9L19 13"/> <rect x="3" y="13" width="18" height="7" rx="2"/> <path d="M7 16.5h.01M11 16.5h6"/>`},
	// Network card.
	"network": {round: true, paths: `<path d="M8 20V5M4 9l4-4 4 4M16 4v15M12 15l4 4 4-4"/>`},
	// Battery card.
	"battery": {round: true, paths: `<rect x="2" y="7" width="17" height="10" rx="2"/> <path d="M22 11v2M6 10v4"/>`},
	// System card.
	"display": {round: true, paths: `<rect x="3" y="4" width="18" height="12" rx="2"/> <path d="M8 20h8M12 16v4"/>`},
	// An app without an icon.
	"app": {round: true, paths: `<rect x="5" y="5" width="14" height="14" rx="4"/> <path d="M9 12h6"/>`},
	// A disk on the 空间透视 start screen.
	"volume": {round: true, paths: `<rect x="3" y="6" width="18" height="12" rx="3"/> <path d="M7 14h.01M11 14h6"/>`},
	// The applications folder.
	"apps": {round: true, paths: `<rect x="4" y="4" width="7" height="7" rx="2"/> <rect x="13" y="4" width="7" height="7" rx="2"/> <rect x="4" y="13" width="7" height="7" rx="2"/> <rect x="13" y="13" width="7" height="7" rx="2"/>`},
	// The home folder.
	"home": {round: true, paths: `<path d="M4 11l8-7 8 7v9h-5v-6h-6v6H4z"/>`},
	// The downloads folder.
	"downloads": {round: true, paths: `<path d="M12 4v11M7 10l5 5 5-5M5 20h14"/>`},
}
