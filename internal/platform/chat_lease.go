package platform

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ChatLease struct {
	ChatID     string    `json:"chatId"`
	RunID      string    `json:"runId"`
	InstanceID string    `json:"instanceId"`
	Epoch      string    `json:"epoch"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func validChatLeaseID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/\\:")
}
func WriteChatLease(root string, l ChatLease) error {
	if !validChatLeaseID(l.ChatID) {
		return errors.New("invalid chat lease identity")
	}
	dir := filepath.Join(root, "leases", "chats")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".lease-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, l.ChatID+".json"))
}
func ReadChatLease(root, id, instance string, now time.Time) (ChatLease, string, error) {
	var l ChatLease
	if !validChatLeaseID(id) {
		return l, LeaseReasonCorrupt, errors.New("invalid chat lease identity")
	}
	b, err := os.ReadFile(filepath.Join(root, "leases", "chats", id+".json"))
	if os.IsNotExist(err) {
		return l, LeaseReasonMissing, nil
	}
	if err != nil {
		return l, LeaseReasonCorrupt, err
	}
	if err = json.Unmarshal(b, &l); err != nil {
		return l, LeaseReasonCorrupt, err
	}
	if l.ChatID != id || l.RunID == "" || l.Epoch == "" {
		return l, LeaseReasonCorrupt, nil
	}
	if l.InstanceID != instance {
		return l, LeaseReasonForeign, nil
	}
	if l.UpdatedAt.IsZero() || now.Sub(l.UpdatedAt) > LeaseTTL || l.UpdatedAt.After(now.Add(LeaseInterval)) {
		return l, LeaseReasonExpired, nil
	}
	return l, LeaseReasonOK, nil
}
func RemoveChatLease(root, id string) {
	if validChatLeaseID(id) {
		_ = os.Remove(filepath.Join(root, "leases", "chats", id+".json"))
	}
}
