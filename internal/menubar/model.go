package menubar

// Item is one native menu row. Rows with Items render as submenus, rows with
// Separator render as separators, and clicking an enabled row with an ID sends
// Action{ID: Item.ID}. Titles are displayed as plain text only.
type Item struct {
	ID        string `json:"id,omitempty"`
	Title     string `json:"title,omitempty"`
	Enabled   bool   `json:"enabled"`
	Checked   bool   `json:"checked,omitempty"`
	Separator bool   `json:"separator,omitempty"`
	Items     []Item `json:"items,omitempty"`
}

// Button is one dialog button; its ID becomes Action.Choice.
type Button struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Dialog is a native alert. The native side shows each unique ID once and
// answers with Action{ID: Dialog.ID, Choice: button ID, Text: input text}.
type Dialog struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Message string   `json:"message"`
	Buttons []Button `json:"buttons"`
	Default string   `json:"default,omitempty"`
	Cancel  string   `json:"cancel,omitempty"`
	Input   bool     `json:"input,omitempty"`
	Value   string   `json:"value,omitempty"`
}

// Model is the complete menu state. Revision must increase monotonically;
// the native side ignores models whose revision it has already applied.
// The native side always appends its own "Hide Menu Bar" row (action "hide").
type Model struct {
	Revision  uint64  `json:"revision"`
	Tooltip   string  `json:"tooltip"`
	Attention bool    `json:"attention"`
	Items     []Item  `json:"items"`
	Dialog    *Dialog `json:"dialog,omitempty"`
}

// Action is a user choice reported by the native menu or a dialog.
type Action struct {
	ID     string `json:"id"`
	Choice string `json:"choice,omitempty"`
	Text   string `json:"text,omitempty"`
}

// HideAction is the Action.ID reported when the user hides the menu bar.
const HideAction = "hide"
