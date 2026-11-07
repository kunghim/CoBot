package collaboration

import "encoding/json"

// toolShape collects a tool's arguments. These are wire schemas, not domain
// permissions. The application still validates identity, ownership, the route
// and execution state.
type toolShape struct {
	props    map[string]any
	required []string
}

func newToolShape() *toolShape {
	return &toolShape{props: map[string]any{}, required: []string{}}
}

func (s *toolShape) add(key string, shape any, mandatory bool) {
	s.props[key] = shape
	if mandatory {
		s.required = append(s.required, key)
	}
}

// json renders the schema. Writes take their idempotency key from the engine's
// call id, so the model never has to invent one.
func (s *toolShape) json() json.RawMessage {
	shape := map[string]any{"type": "object", "properties": s.props, "required": s.required, "additionalProperties": false}
	data, err := json.Marshal(shape)
	if err != nil {
		panic(err)
	}
	return data
}

func textProp(description string) any {
	return map[string]any{"type": "string", "description": description, "minLength": 1}
}

func stringsProp(description string) any {
	return map[string]any{"type": "array", "description": description, "items": map[string]any{"type": "string", "minLength": 1}}
}
