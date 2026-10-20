package sqlite

import (
	"context"
	"testing"

	"github.com/kunghim/CoBot/internal/domain"
)

func seedProvider() domain.Provider {
	return domain.Provider{
		ID:       "cobot_deepseek",
		Name:     "DeepSeek",
		Kind:     domain.ProviderKindCustom,
		AuthMode: domain.ProviderAuthAPIKey,
		Endpoints: []domain.ProviderEndpoint{
			{Dialect: domain.DialectOpenAIResponses, BaseURL: "https://api.deepseek.com/v1"},
			{Dialect: domain.DialectAnthropicMessages, BaseURL: "https://api.deepseek.com/anthropic"},
		},
		Models: []domain.ProviderModel{
			{ID: "deepseek-v4-pro", ContextWindow: 1000000, Status: domain.ModelStatusOK},
			{ID: "deepseek-flash", Status: domain.ModelStatusFailed, Failure: "quota", Message: "余额不足"},
		},
		DefaultModel:  "deepseek-v4-pro",
		CredentialRef: "provider_cobot_deepseek",
		PresetID:      "deepseek",
		Retries:       domain.ProviderRetries{Request: 1, Stream: 2},
	}
}

func TestProviderRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)
	want := seedProvider()

	saved, err := UpsertProvider(ctx, db, want, 0)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 1 {
		t.Fatalf("new provider revision = %d, want 1", saved.Revision)
	}
	if len(saved.Endpoints) != 2 || saved.Endpoints[1].Dialect != domain.DialectAnthropicMessages {
		t.Fatalf("endpoints did not survive: %+v", saved.Endpoints)
	}
	if len(saved.Models) != 2 {
		t.Fatalf("models did not survive: %+v", saved.Models)
	}
	if saved.Models[1].Status != domain.ModelStatusFailed || saved.Models[1].Failure != "quota" {
		t.Fatalf("model status did not survive: %+v", saved.Models[1])
	}
	if saved.Retries.Request != 1 || saved.Retries.Stream != 2 {
		t.Fatalf("retries did not survive: %+v", saved.Retries)
	}
	if saved.PresetID != "deepseek" {
		t.Fatalf("preset id did not survive: %q", saved.PresetID)
	}

	got, err := GetProvider(ctx, db, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != want.Name || got.DefaultModel != want.DefaultModel {
		t.Fatalf("provider changed on the way back: %+v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatal("timestamps were not stored")
	}
}

func TestProviderUpdateBumpsRevisionAndRefusesStaleWrites(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)
	want := seedProvider()
	saved, err := UpsertProvider(ctx, db, want, 0)
	if err != nil {
		t.Fatal(err)
	}

	name := "改名"
	updated, err := UpsertProvider(ctx, db, saved.Apply(domain.ProviderPatch{Name: &name}), saved.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != saved.Revision+1 {
		t.Fatalf("revision = %d, want %d", updated.Revision, saved.Revision+1)
	}
	if updated.Name != name {
		t.Fatalf("name not applied: %q", updated.Name)
	}

	// The second write carries the revision it read, which is now stale.
	other := "再改"
	if _, err := UpsertProvider(ctx, db, updated.Apply(domain.ProviderPatch{Name: &other}), saved.Revision); err == nil {
		t.Fatal("a stale revision was accepted")
	}
}

func TestProviderListAndDelete(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)
	if _, err := UpsertProvider(ctx, db, seedProvider(), 0); err != nil {
		t.Fatal(err)
	}
	second := seedProvider()
	second.ID = "cobot_kimi"
	second.Name = "Kimi"
	if _, err := UpsertProvider(ctx, db, second, 0); err != nil {
		t.Fatal(err)
	}

	list, err := ListProviders(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("listed %d providers, want 2", len(list))
	}
	for _, p := range list {
		if p.ID == "" || p.Name == "" {
			t.Fatalf("incomplete provider in the list: %+v", p)
		}
	}

	if err := DeleteProvider(ctx, db, "cobot_kimi"); err != nil {
		t.Fatal(err)
	}
	if _, err := GetProvider(ctx, db, "cobot_kimi"); err == nil {
		t.Fatal("a deleted provider is still readable")
	}
	if err := DeleteProvider(ctx, db, "cobot_kimi"); err == nil {
		t.Fatal("deleting a provider twice succeeded")
	}
}

func TestProviderRejectsAnInvalidRecord(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)
	bad := seedProvider()
	bad.Endpoints = nil
	if _, err := UpsertProvider(ctx, db, bad, 0); err == nil {
		t.Fatal("a provider without an endpoint was stored")
	}
	list, err := ListProviders(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("a rejected provider was stored anyway: %d rows", len(list))
	}
}

func TestSettingsCarryTheDefaultProvider(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)
	id := "cobot_deepseek"
	providerID := "cobot_deepseek"
	patch := domain.SettingsPatch{DefaultProvider: &providerID}
	saved, err := UpdateSettings(ctx, db, patch, 0)
	if err != nil {
		t.Fatal(err)
	}
	if saved.DefaultProvider != providerID {
		t.Fatalf("default provider = %q, want %q", saved.DefaultProvider, providerID)
	}
	again, err := GetSettings(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if again.DefaultProvider != providerID {
		t.Fatalf("default provider did not survive: %q", again.DefaultProvider)
	}
	_ = id
}
