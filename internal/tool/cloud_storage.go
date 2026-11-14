package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/u007/ocode/internal/cloudconnect"
)

type CloudStorageTool struct{}

func (CloudStorageTool) Name() string { return "cloud_storage" }

func (CloudStorageTool) Description() string {
	return "Browse, read, write, and delete files in configured cloud storage connectors (Google Drive, OneDrive, GCS, S3, Dropbox). Actions: list_connectors, browse, read, write, delete."
}

func (CloudStorageTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "cloud_storage",
		"description": "Browse, read, write, and delete files in configured cloud storage connectors.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"list_connectors", "browse", "read", "write", "delete"},
					"description": "Action to perform.",
				},
				"connector_id": map[string]interface{}{
					"type":        "string",
					"description": "Connector id (for browse/read/write/delete).",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path within the connector (for browse/read/write/delete).",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "File content for write action.",
				},
			},
			"required": []string{"action"},
		},
	}
}

func (CloudStorageTool) Parallel() bool { return false }

func (t CloudStorageTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (t CloudStorageTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	mgr := cloudconnect.DefaultManager
	if mgr == nil {
		return "", fmt.Errorf("cloud connectors not initialized")
	}
	var in struct {
		Action      string `json:"action"`
		ConnectorID string `json:"connector_id"`
		Path        string `json:"path"`
		Content     string `json:"content"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	switch in.Action {
	case "list_connectors":
		b, _ := json.MarshalIndent(mgr.List(), "", "  ")
		return string(b), nil
	case "browse":
		entries, err := mgr.Browse(ctx, in.ConnectorID, in.Path)
		if err != nil {
			return "", err
		}
		b, _ := json.MarshalIndent(entries, "", "  ")
		return string(b), nil
	case "read":
		rc, err := mgr.Read(ctx, in.ConnectorID, in.Path)
		if err != nil {
			return "", err
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "write":
		err := mgr.Write(ctx, in.ConnectorID, in.Path, strings.NewReader(in.Content), int64(len(in.Content)))
		if err != nil {
			return "", err
		}
		return "written", nil
	case "delete":
		if err := mgr.Delete(ctx, in.ConnectorID, in.Path); err != nil {
			return "", err
		}
		return "deleted", nil
	default:
		return "", fmt.Errorf("unknown action %q", in.Action)
	}
}
