package app

import (
	"context"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/modelcatalog"
	"github.com/kunghim/CoBot/internal/roles"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

func appWithCatalog(t *testing.T) *App {
	t.Helper()
	a := providerTestApp(t, &fakeBinder{})
	store, err := modelcatalog.Open("")
	if err != nil {
		t.Fatal(err)
	}
	a.ModelCatalog = store
	return a
}

func TestACatalogModelTakesWhatItKnowsAndTheUserMayAskForLess(t *testing.T) {
	a := appWithCatalog(t)
	// Nothing entered: the catalog's numbers, and its thinking, which stays the catalog's.
	spec := a.resolveModel(domain.ProviderModel{ID: "q-glm-5.3", DisplayName: "我的 GLM", SupportedEfforts: []string{"minimal"}, WebSearch: true})
	if spec.Source != "catalog" || spec.ContextWindow != 1000000 || spec.ContextLimit != 1000000 || spec.DisplayName != "我的 GLM" {
		t.Fatalf("spec = %+v", spec)
	}
	if !reflect.DeepEqual(spec.Thinking.Efforts, []string{"low", "high", "max"}) || spec.Thinking.Toggle {
		t.Fatalf("thinking = %+v", spec.Thinking)
	}
	if !reflect.DeepEqual(spec.Locked, []string{lockedThinking}) {
		t.Errorf("locked = %v", spec.Locked)
	}
	if !spec.WebSearch {
		t.Error("the user's web search choice was lost")
	}
	// Less is taken, more is not.
	if got := a.resolveModel(domain.ProviderModel{ID: "glm-5.3", ContextWindow: 200000, MaxOutput: 8000}); got.ContextWindow != 200000 || got.MaxOutput != 8000 || got.ContextLimit != 1000000 {
		t.Errorf("a lowered window = %+v", got)
	}
	if got := a.resolveModel(domain.ProviderModel{ID: "glm-5.3", ContextWindow: 5000000, MaxOutput: 99999999}); got.ContextWindow != 1000000 || got.MaxOutput != got.MaxOutputLimit {
		t.Errorf("a raised window = %+v", got)
	}
	// Kinds of input: a subset is taken, a kind the model lacks is not.
	flash := a.resolveModel(domain.ProviderModel{ID: "qwen3.8-flash", InputModalities: []string{"text", "pdf"}})
	if !reflect.DeepEqual(flash.InputModalities, []string{"text"}) || !contains(flash.InputOptions, "image") {
		t.Errorf("input = %v of %v", flash.InputModalities, flash.InputOptions)
	}
	if got := a.resolveModel(domain.ProviderModel{ID: "qwen3.8-flash"}); !contains(got.InputModalities, "image") {
		t.Errorf("nothing entered must be the catalog's kinds: %v", got.InputModalities)
	}
}

func TestAModelTheCatalogDoesNotKnowIsTheUsersToDescribe(t *testing.T) {
	a := appWithCatalog(t)
	spec := a.resolveModel(domain.ProviderModel{ID: "my-own-model-x", ContextWindow: 64000, InputModalities: []string{"image"}, ThinkingToggle: true})
	if spec.Source != "custom" || len(spec.Locked) != 0 || spec.ContextWindow != 64000 {
		t.Fatalf("spec = %+v", spec)
	}
	if !reflect.DeepEqual(spec.InputModalities, []string{"text", "image"}) || !spec.Thinking.Toggle || len(spec.Thinking.Efforts) != 0 {
		t.Fatalf("spec = %+v", spec)
	}
	// Told nothing about thinking, it has the three usual levels.
	plain := a.resolveModel(domain.ProviderModel{ID: "my-own-model-x"})
	if !reflect.DeepEqual(plain.Thinking.Efforts, domain.DefaultEfforts) {
		t.Fatalf("default levels = %v", plain.Thinking.Efforts)
	}
}

func TestWhatTheCatalogAnswersForIsNotStored(t *testing.T) {
	a := appWithCatalog(t)
	p := testProvider()
	p.Models = []domain.ProviderModel{
		{ID: "glm-5.3", DisplayName: "GLM", ContextWindow: 5000000, MaxOutput: 8000, SupportedEfforts: []string{"low"}, WebSearch: true},
		{ID: "my-own-model-x", ContextWindow: 5},
	}
	saved, err := a.SaveProvider(context.Background(), p, 0)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := sqlite.GetProvider(context.Background(), a.DB, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	known, own := stored.Models[0], stored.Models[1]
	if known.ContextWindow != 0 || known.MaxOutput != 8000 || known.SupportedEfforts != nil || known.Spec != nil || known.DisplayName != "GLM" || !known.WebSearch {
		t.Fatalf("a model the catalog knows = %+v", known)
	}
	if own.ContextWindow != 5 {
		t.Fatalf("a model it does not know = %+v", own)
	}
}

func TestAThinkingSwitchOffIsOnlySentToAModelWithOne(t *testing.T) {
	ctx := context.Background()
	a := appWithCatalog(t)
	p := testProvider()
	p.Models = []domain.ProviderModel{{ID: "deepseek-v4-pro"}, {ID: "glm-5.3"}}
	saved, err := a.SaveProvider(ctx, p, 0)
	if err != nil {
		t.Fatal(err)
	}
	pro := ModelChoice{ProviderID: saved.ID, Model: "deepseek-v4-pro"}
	glm := ModelChoice{ProviderID: saved.ID, Model: "glm-5.3"}
	if got := a.effortFor(ctx, pro, "", "none"); got != "none" {
		t.Errorf("deepseek none = %q", got)
	}
	if got := a.effortFor(ctx, glm, "", "none"); got != "" {
		t.Errorf("glm none = %q, want it withheld", got)
	}
	if got := a.effortFor(ctx, glm, "", "max"); got != "max" {
		t.Errorf("glm max = %q", got)
	}
	if got := a.effortFor(ctx, glm, "", "medium"); got != "" {
		t.Errorf("glm medium = %q", got)
	}
}

type catalogServer struct {
	engine.Server
	fingerprint string
	stopped     chan struct{}
}

func (c *catalogServer) CatalogFingerprint() string { return c.fingerprint }
func (c *catalogServer) Shutdown(context.Context) error {
	close(c.stopped)
	return nil
}

func TestEnginesOnAnOlderCatalogAreStoppedWhenIdle(t *testing.T) {
	ctx := context.Background()
	a := providerTestApp(t, &fakeBinder{})
	a.Logger = slog.Default()
	s := NewScheduler(a, nil)
	old := &catalogServer{fingerprint: "old", stopped: make(chan struct{})}
	current := &catalogServer{fingerprint: "new", stopped: make(chan struct{})}
	s.teams["team-old"] = &teamRuntime{server: old}
	s.teams["team-new"] = &teamRuntime{server: current}
	if got := s.StaleEngines("new"); got != 1 {
		t.Fatalf("stale = %d, want 1", got)
	}
	stopped, busy := s.RestartStaleEngines(ctx, "new")
	if stopped != 1 || busy != 0 {
		t.Fatalf("stopped %d busy %d", stopped, busy)
	}
	select {
	case <-old.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("the engine on the old catalog was not shut down")
	}
	select {
	case <-current.stopped:
		t.Fatal("an engine on the current catalog was stopped")
	default:
	}
	if got := s.StaleEngines("new"); got != 0 {
		t.Fatalf("stale after = %d", got)
	}
}

func TestAModelWithoutThePatchToolIsToldHowToEditFiles(t *testing.T) {
	windows := fileEditGuide("windows")
	if !strings.Contains(windows, "apply_patch") || !strings.Contains(windows, "Set-Content") || strings.Contains(windows, "sed") {
		t.Fatalf("windows guide = %q", windows)
	}
	for _, goos := range []string{"darwin", "linux"} {
		unix := fileEditGuide(goos)
		if !strings.Contains(unix, "apply_patch <<") || !strings.Contains(unix, "perl -pi") || strings.Contains(unix, "Set-Content") || strings.Contains(unix, "不要在命令行里调用") {
			t.Fatalf("%s guide = %q", goos, unix)
		}
	}
}

func TestTheCatalogsNameIsNotKeptAsAnAlias(t *testing.T) {
	a := appWithCatalog(t)
	spec := a.resolveModel(domain.ProviderModel{ID: "q-glm-5.3"})
	cleared := a.clearedOfWhatTheCatalogGives([]domain.ProviderModel{
		{ID: "q-glm-5.3", DisplayName: spec.Name},
		{ID: "glm-5.3", DisplayName: "my own name"},
	})
	if cleared[0].DisplayName != "" || cleared[1].DisplayName != "my own name" {
		t.Fatalf("aliases = %q / %q", cleared[0].DisplayName, cleared[1].DisplayName)
	}
}

func TestAModelThatIsNotTheAccountsIsToldNotToWriteToolCallsAsText(t *testing.T) {
	a := providerTestApp(t, &fakeBinder{})
	a.Logger = slog.Default()
	team := domain.Team{ID: "t", Name: "T", Directory: "C:/x"}
	bot := domain.Bot{ID: "b", TeamID: "t", Name: "PM", IsLeader: true}
	role := roles.Role{ID: "leader", Name: "项目经理"}
	other, _ := a.threadInstructions(context.Background(), team, bot, role, false)
	official, _ := a.threadInstructions(context.Background(), team, bot, role, true)
	if !strings.Contains(other, "<tool_call>") || strings.Contains(official, "<tool_call>") {
		t.Fatalf("other has the line: %v, official: %v", strings.Contains(other, "<tool_call>"), strings.Contains(official, "<tool_call>"))
	}
}
