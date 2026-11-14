// Package fulldisk tells whether Spruce holds Full Disk Access (macOS) and guides the user to System Settings to give it. Windows has no such permission: there every answer is NotRequired.
package fulldisk

// State is whether the app holds Full Disk Access.
type State uint8

const (
	// Unknown means the system cannot tell.
	Unknown State = iota
	// Granted means the app holds the permission.
	Granted
	// Denied means the user has not given it.
	Denied
	// NotRequired means the platform has no such permission.
	NotRequired
)

// String names the state, for logs.
func (s State) String() string {
	switch s {
	case Granted:
		return "granted"
	case Denied:
		return "denied"
	case NotRequired:
		return "notRequired"
	}
	return "unknown"
}
