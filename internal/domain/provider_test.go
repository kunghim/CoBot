package domain

import "testing"

// validProvider returns a provider that passes validation, so a test can
// break exactly one thing at a time.
func validProvider() Provider {
	return Provider{
		ID:       "cobot_deepseek",
		Name:     "DeepSeek",
		Kind:     ProviderKindCustom,
		AuthMode: ProviderAuthAPIKey,
		Endpoints: []ProviderEndpoint{
			{Dialect: DialectOpenAIResponses, BaseURL: "https://api.deepseek.com/v1"},
		},
		Models:        []ProviderModel{{ID: "deepseek-v4-pro"}},
		DefaultModel:  "deepseek-v4-pro",
		CredentialRef: "provider_cobot_deepseek",
	}
}

func TestProviderValidateAcceptsAUsableProvider(t *testing.T) {
	p := validProvider()
	if err := p.Validate(); err != nil {
		t.Fatalf("valid provider rejected: %v", err)
	}
}

func TestProviderValidateRejectsUnusableProviders(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Provider)
	}{
		{"empty id", func(p *Provider) { p.ID = "" }},
		{"id with a path separator", func(p *Provider) { p.ID = "a/b" }},
		{"id with a dot", func(p *Provider) { p.ID = "a.b" }},
		{"id starting with a dash", func(p *Provider) { p.ID = "-a" }},
		{"empty name", func(p *Provider) { p.Name = "  " }},
		{"unknown kind", func(p *Provider) { p.Kind = "borrowed" }},
		{"unknown auth mode", func(p *Provider) { p.AuthMode = "oauth" }},
		{"api key without a credential", func(p *Provider) { p.CredentialRef = "" }},
		{"chatgpt with a credential", func(p *Provider) { p.AuthMode = ProviderAuthChatGPT }},
		{"no endpoint", func(p *Provider) { p.Endpoints = nil }},
		{"unknown dialect", func(p *Provider) { p.Endpoints[0].Dialect = "grpc" }},
		{"relative endpoint", func(p *Provider) { p.Endpoints[0].BaseURL = "/v1" }},
		{"endpoint without a scheme", func(p *Provider) { p.Endpoints[0].BaseURL = "api.deepseek.com/v1" }},
		{"endpoint with a foreign scheme", func(p *Provider) { p.Endpoints[0].BaseURL = "ftp://api.deepseek.com/v1" }},
		{"empty endpoint", func(p *Provider) { p.Endpoints[0].BaseURL = "  " }},
		{"model without an id", func(p *Provider) { p.Models = []ProviderModel{{}} }},
		{"duplicate model", func(p *Provider) { p.Models = []ProviderModel{{ID: "m"}, {ID: "m"}} }},
		{"default model that was not kept", func(p *Provider) { p.DefaultModel = "other" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validProvider()
			tc.mutate(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("invalid provider accepted")
			}
		})
	}
}

func TestProviderOfficialNeedsNoCredentialAndNoEndpoint(t *testing.T) {
	// The official provider inherits the engine's own provider and login
	// state, so it has neither a secret nor an endpoint of its own.
	p := Provider{
		ID:       "openai",
		Name:     "OpenAI 官方",
		Kind:     ProviderKindOfficial,
		AuthMode: ProviderAuthChatGPT,
		Models:   []ProviderModel{{ID: "gpt-6.1-sol"}},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("official provider rejected: %v", err)
	}
}

func TestProviderEndpointLookup(t *testing.T) {
	p := validProvider()
	p.Endpoints = append(p.Endpoints, ProviderEndpoint{
		Dialect: DialectAnthropicMessages,
		BaseURL: "https://api.deepseek.com/anthropic",
	})
	if !p.SupportsDialect(DialectOpenAIResponses) || !p.SupportsDialect(DialectAnthropicMessages) {
		t.Fatal("provider lost a dialect it declares")
	}
	if p.SupportsDialect("grpc") {
		t.Fatal("provider claims a dialect it does not declare")
	}
	ep, ok := p.EndpointFor(DialectAnthropicMessages)
	if !ok || ep.BaseURL != "https://api.deepseek.com/anthropic" {
		t.Fatalf("wrong endpoint: %+v", ep)
	}
	if _, ok := p.EndpointFor("grpc"); ok {
		t.Fatal("endpoint lookup invented a dialect")
	}
}

func TestProviderKeepsModel(t *testing.T) {
	p := validProvider()
	if !p.KeepsModel("deepseek-v4-pro") {
		t.Fatal("kept model not found")
	}
	if p.KeepsModel("deepseek-flash") {
		t.Fatal("model the user did not keep is reported as kept")
	}
}

func TestProviderApplyLeavesUnmentionedFieldsAlone(t *testing.T) {
	p := validProvider()
	name := "改过的名字"
	updated := p.Apply(ProviderPatch{Name: &name})
	if updated.Name != name {
		t.Fatalf("name not applied: %q", updated.Name)
	}
	if updated.Endpoints[0].BaseURL != p.Endpoints[0].BaseURL {
		t.Fatal("patch changed the endpoints it did not mention")
	}
	if updated.Models[0].ID != p.Models[0].ID {
		t.Fatal("patch changed the models it did not mention")
	}
}

func TestAModelMayDeclareOnlyValidThinkingLevels(t *testing.T) {
	provider := func(model ProviderModel) Provider {
		return Provider{
			ID: "gw", Name: "Gateway", Kind: ProviderKindCustom, AuthMode: ProviderAuthAPIKey, CredentialRef: "ref",
			Endpoints: []ProviderEndpoint{{Dialect: DialectOpenAIResponses, BaseURL: "https://gw.example/v1"}},
			Models:    []ProviderModel{model},
		}
	}
	good := []ProviderModel{
		{ID: "m"},
		{ID: "m", SupportedEfforts: []string{"low", "high", "xhigh"}, DefaultEffort: "high"},
	}
	for _, m := range good {
		if err := provider(m).Validate(); err != nil {
			t.Errorf("%+v rejected: %v", m, err)
		}
	}
	bad := []ProviderModel{
		{ID: "m", SupportedEfforts: []string{""}},
		{ID: "m", SupportedEfforts: []string{"High!"}},
		{ID: "m", SupportedEfforts: []string{"low", "low"}},
		{ID: "m", SupportedEfforts: []string{"low"}, DefaultEffort: "high"},
		{ID: "m", DefaultEffort: "low"},
	}
	for _, m := range bad {
		if err := provider(m).Validate(); err == nil {
			t.Errorf("%+v accepted", m)
		}
	}
}

func TestAModelThatListsNoThinkingLevelsHasTheDefaultThree(t *testing.T) {
	if got := (ProviderModel{ID: "m"}).SupportedEffortsOf(); len(got) != 3 || got[0] != "low" || got[1] != "medium" || got[2] != "high" {
		t.Fatalf("defaults = %v", got)
	}
	if got := (ProviderModel{ID: "m", SupportedEfforts: []string{"xhigh"}}).SupportedEffortsOf(); len(got) != 1 || got[0] != "xhigh" {
		t.Fatalf("listed = %v", got)
	}
}
