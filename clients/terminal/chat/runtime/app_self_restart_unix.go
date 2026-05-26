//go:build !windows

package runtime

import (
	"io"
	"os"
	"syscall"

	tea "charm.land/bubbletea/v2"
)

type terminalRestartCommand struct {
	path string
	args []string
	env  []string
}

func (c terminalRestartCommand) Run() error {
	return syscall.Exec(c.path, c.args, c.env)
}

func (c terminalRestartCommand) SetStdin(io.Reader)  {}
func (c terminalRestartCommand) SetStdout(io.Writer) {}
func (c terminalRestartCommand) SetStderr(io.Writer) {}

func (m *appModel) restartTerminalCmd() tea.Cmd {
	exe, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return terminalRestartMsg{err: err} }
	}
	args := append([]string{exe}, os.Args[1:]...)
	return tea.Exec(terminalRestartCommand{
		path: exe,
		args: args,
		env:  os.Environ(),
	}, func(err error) tea.Msg {
		return terminalRestartMsg{err: err}
	})
}
