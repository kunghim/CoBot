package collaboration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Older Codex builds supply native turn metadata but omit _meta.callId.
// Preserve the MCP request identity before the SDK's typed handler drops it.
// The native thread and turn namespace IDs that a retained/restarted MCP
// connection can reuse; tool arguments never establish this identity.
type callIDTransport struct{ mcp.Transport }

func (t callIDTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := t.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return callIDConnection{conn}, nil
}

type callIDConnection struct{ mcp.Connection }

func (c callIDConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	message, err := c.Connection.Read(ctx)
	if request, ok := message.(*jsonrpc.Request); err == nil && ok {
		preserveCallID(request)
	}
	return message, err
}

func preserveCallID(request *jsonrpc.Request) {
	if request.Method != "tools/call" || !request.ID.IsValid() {
		return
	}
	var params map[string]json.RawMessage
	if json.Unmarshal(request.Params, &params) != nil {
		return
	}
	var meta map[string]any
	if json.Unmarshal(params["_meta"], &meta) != nil {
		return
	}
	if existing, _ := meta["callId"].(string); existing != "" {
		return
	}
	thread, turn := nativeTurnIdentity(meta)
	if thread == "" || turn == "" {
		return
	}
	identity, _ := json.Marshal([]any{thread, turn, request.ID.Raw()})
	hash := sha256.Sum256(identity)
	meta["callId"] = "mcp:" + hex.EncodeToString(hash[:])
	params["_meta"], _ = json.Marshal(meta)
	request.Params, _ = json.Marshal(params)
}
