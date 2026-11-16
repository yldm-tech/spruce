package shell

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/nsgo-dev/nsgo/core"
)

// frameFile is where the window's frame is kept in the data folder.
const frameFile = "window.json"

// savedFrame is window.json: the window's top left corner from the main screen's top left and its outer size, in points.
type savedFrame struct {
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

// LoadFrame returns the frame the window had as the app last quit, for nsgo.WindowFrame, and false when none was kept or it cannot be read. NSGo opens a frame no screen shows centered on the main screen.
func LoadFrame(dataDir string) (core.WindowFrame, bool) {
	if dataDir == "" {
		return core.WindowFrame{}, false
	}
	data, err := os.ReadFile(filepath.Join(dataDir, frameFile))
	if err != nil {
		return core.WindowFrame{}, false
	}
	var f savedFrame
	if json.Unmarshal(data, &f) != nil || f.Width <= 0 || f.Height <= 0 {
		return core.WindowFrame{}, false
	}
	return core.WindowFrame{X: core.Points(f.X), Y: core.Points(f.Y), Move: true, Width: core.Points(f.Width), Height: core.Points(f.Height)}, true
}

// SaveFrame keeps f in the data folder; a frame without a size is not kept.
func SaveFrame(dataDir string, f core.WindowFrame) error {
	if dataDir == "" || f.Width <= 0 || f.Height <= 0 {
		return nil
	}
	data, err := json.Marshal(savedFrame{X: float32(f.X), Y: float32(f.Y), Width: float32(f.Width), Height: float32(f.Height)})
	if err != nil {
		return err
	}
	tmp := filepath.Join(dataDir, frameFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dataDir, frameFile)); err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	return nil
}
