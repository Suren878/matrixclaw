package mcp

import (
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// PermissionSubject names the server and its tool as "server__tool".
func (e *remoteToolExecutor) PermissionSubject(tools.Call) permission.Subject {
	return permission.Subject{Kind: permission.KindName, Value: e.server.ID + "__" + e.remoteName}
}
