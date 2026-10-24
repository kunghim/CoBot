// Package modelcatalog knows what is publicly known about models: how much
// context one has, what it takes as input, and how its thinking is switched.
//
// The facts come from models.dev (an open dataset, MIT licensed), converted to
// the few fields CoBot uses. A catalog ships with the program, and the user can
// replace it with a newer one downloaded from the same place. It is trusted as
// it is: a model it knows has its values taken from it and not edited by hand.
package modelcatalog

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SourceURL is where the dataset is downloaded from.
const SourceURL = "https://models.dev/api.json"

// vendors are the dataset's providers that are read, most authoritative first.
// A model id that more than one of them lists is taken from the first. The
// list is the vendors whose models are reached through gateways and endpoints
// of other names; everything else in the dataset is left out to keep the file
// small.
var vendors = []string{
	"xiaomi", "zhipuai", "deepseek", "alibaba", "moonshotai", "minimax",
	"zai", "alibaba-cn", "moonshotai-cn", "minimax-cn",
	"openai", "anthropic", "google",
}

// Thinking is how a model's thinking is controlled.
type Thinking struct {
	// Toggle is true when thinking can be switched off (and on).
	Toggle bool `json:"toggle,omitempty"`
	// Efforts are the levels it can be set to, lowest first; empty when it has
	// no levels. A model with neither a toggle nor levels thinks as it does.
	Efforts []string `json:"efforts,omitempty"`
}

// Entry is one model.
type Entry struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Vendor string `json:"vendor"`
	// Context is the context window in tokens, Output the most it answers with.
	Context int `json:"context,omitempty"`
	Output  int `json:"output,omitempty"`
	// Input is what it takes: text, image, video, pdf, audio.
	Input []string `json:"input,omitempty"`
	// Thinking is nil when the dataset says nothing about thinking.
	Thinking *Thinking `json:"thinking,omitempty"`
}

// Catalog is a set of models and where it came from.
type Catalog struct {
	Version   int       `json:"version"`
	Source    string    `json:"source"`
	FetchedAt time.Time `json:"fetchedAt"`
	Models    []Entry   `json:"models"`

	byID   map[string]*Entry
	suffix []*Entry // longest id first, for ids a gateway prefixed
}

const formatVersion = 1

type rawModel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Reasoning  *bool  `json:"reasoning"`
	Reasonings []struct {
		Type   string   `json:"type"`
		Values []string `json:"values"`
	} `json:"reasoning_options"`
	Modalities struct {
		Input []string `json:"input"`
	} `json:"modalities"`
	Limit struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
}

// Convert reads the dataset and keeps what CoBot uses. A model or vendor it
// cannot read is skipped rather than failing the whole conversion, so a
// change in one corner of the dataset does not take the catalog away.
func Convert(raw []byte, now time.Time) (*Catalog, error) {
	var providers map[string]struct {
		Models map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(raw, &providers); err != nil {
		return nil, fmt.Errorf("the dataset is not a JSON object of vendors: %w", err)
	}
	catalog := &Catalog{Version: formatVersion, Source: SourceURL, FetchedAt: now.UTC()}
	seen := map[string]bool{}
	for _, vendor := range vendors {
		provider, ok := providers[vendor]
		if !ok {
			continue
		}
		ids := make([]string, 0, len(provider.Models))
		for id := range provider.Models {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			var model rawModel
			if json.Unmarshal(provider.Models[id], &model) != nil {
				continue
			}
			if model.ID == "" {
				model.ID = id
			}
			key := strings.ToLower(model.ID)
			if seen[key] {
				continue
			}
			seen[key] = true
			catalog.Models = append(catalog.Models, entryOf(vendor, model))
		}
	}
	if len(catalog.Models) == 0 {
		return nil, fmt.Errorf("the dataset lists none of the models CoBot reads")
	}
	catalog.index()
	return catalog, nil
}

func entryOf(vendor string, model rawModel) Entry {
	entry := Entry{ID: model.ID, Name: model.Name, Vendor: vendor, Context: model.Limit.Context, Output: model.Limit.Output}
	for _, kind := range model.Modalities.Input {
		entry.Input = append(entry.Input, strings.ToLower(kind))
	}
	if model.Reasoning == nil {
		return entry
	}
	thinking := &Thinking{}
	if *model.Reasoning {
		for _, option := range model.Reasonings {
			switch option.Type {
			case "toggle":
				thinking.Toggle = true
			case "effort":
				for _, value := range option.Values {
					// "none" is how the dataset spells turning thinking off.
					if value == "none" {
						thinking.Toggle = true
						continue
					}
					if !contains(thinking.Efforts, value) {
						thinking.Efforts = append(thinking.Efforts, value)
					}
				}
			}
		}
	}
	entry.Thinking = thinking
	return entry
}

func contains(list []string, value string) bool {
	for _, entry := range list {
		if entry == value {
			return true
		}
	}
	return false
}

// index prepares the lookups; it runs after a catalog is converted or read.
func (c *Catalog) index() {
	c.byID = make(map[string]*Entry, len(c.Models))
	c.suffix = c.suffix[:0]
	for i := range c.Models {
		entry := &c.Models[i]
		c.byID[strings.ToLower(entry.ID)] = entry
		c.suffix = append(c.suffix, entry)
	}
	sort.SliceStable(c.suffix, func(i, j int) bool { return len(c.suffix[i].ID) > len(c.suffix[j].ID) })
}

// normalize makes a model id comparable: lower case, without a vendor path in
// front or a tag (":free") behind.
func normalize(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if cut := strings.Index(id, ":"); cut >= 0 {
		id = id[:cut]
	}
	if cut := strings.LastIndex(id, "/"); cut >= 0 {
		id = id[cut+1:]
	}
	return id
}

// Match finds the entry for a model id. An id matches an entry when they are
// equal, or when the id is the entry's id with a gateway's own word in front
// ("q-glm-5.3" for "glm-5.3"); of several entries the longest id wins, so
// "q-glm-5.3" is not taken for "glm-5".
func (c *Catalog) Match(id string) (Entry, bool) {
	if c == nil {
		return Entry{}, false
	}
	key := normalize(id)
	if key == "" {
		return Entry{}, false
	}
	if entry, ok := c.byID[key]; ok {
		return *entry, true
	}
	for _, entry := range c.suffix {
		candidate := strings.ToLower(entry.ID)
		if len(candidate) < len(key) && (strings.HasSuffix(key, "-"+candidate) || strings.HasSuffix(key, "_"+candidate)) {
			return *entry, true
		}
	}
	return Entry{}, false
}

// Parse reads a catalog written by Encode.
func Parse(raw []byte) (*Catalog, error) {
	var catalog Catalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil, err
	}
	if catalog.Version != formatVersion || len(catalog.Models) == 0 {
		return nil, fmt.Errorf("not a model catalog this version reads")
	}
	catalog.index()
	return &catalog, nil
}

// Encode writes the catalog for storing.
func (c *Catalog) Encode() ([]byte, error) {
	return json.Marshal(c)
}
