package execx

import "os/exec"

// configure applies platform settings to cmd before it starts; macOS needs none.
func configure(*exec.Cmd) {}
