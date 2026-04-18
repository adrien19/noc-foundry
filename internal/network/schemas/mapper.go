// Copyright 2026 Adrien Ndikumana
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package schemas

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/adrien19/noc-foundry/internal/network/models"
)

// SchemaMapper maps raw gNMI JSON or NETCONF XML responses into canonical
// model types using declarative field mappings. It is vendor-agnostic: the
// same mapper instance handles OpenConfig, Nokia SRL, Nokia SROS, and any
// other vendor whose YANG leaf names are in the canonical map.
type SchemaMapper struct {
	bundle *SchemaBundle
	cmap   *OperationCanonicalMap

	// fieldIndex maps YANGLeaf → FieldMapping for O(1) lookup during walk.
	fieldIndex map[string]FieldMapping
	// aliasSet contains container names that should be merged upward.
	aliasSet      map[string]bool
	childMappings []ChildMapping
}

// NewSchemaMapper creates a mapper for the given operation and schema bundle.
// Returns an error if no canonical map is registered for the operation.
func NewSchemaMapper(bundle *SchemaBundle, operationID string) (*SchemaMapper, error) {
	cmap, ok := LookupCanonicalMap(operationID)
	if !ok {
		return nil, fmt.Errorf("no canonical map registered for operation %q", operationID)
	}

	fieldIndex := make(map[string]FieldMapping, len(cmap.Fields))
	for _, f := range cmap.Fields {
		fieldIndex[f.YANGLeaf] = f
	}

	aliasSet := make(map[string]bool, len(cmap.ContainerAliases))
	for _, a := range cmap.ContainerAliases {
		if a.MergeUp {
			aliasSet[a.Name] = true
		}
	}

	return &SchemaMapper{
		bundle:        bundle,
		cmap:          cmap,
		fieldIndex:    fieldIndex,
		aliasSet:      aliasSet,
		childMappings: cmap.ChildMappings,
	}, nil
}

// MapJSON maps an unmarshalled JSON value (typically from gNMI JSON_IETF)
// into canonical model types. It handles:
//   - Top-level arrays of objects (list of interfaces)
//   - Vendor-namespaced wrappers (e.g. {"srl_nokia-interfaces:interface": [...]})
//   - OpenConfig nested state/config containers
//   - Flat vendor models (Nokia SRL)
//
// Returns a typed payload (e.g. []models.InterfaceState) and quality metadata.
func (m *SchemaMapper) MapJSON(data any) (any, models.QualityMeta, error) {
	// Unwrap vendor-namespaced wrappers recursively.
	items := m.unwrapToList(data)

	descriptor, ok := LookupCanonicalModel(m.cmap.OperationID)
	if !ok {
		return nil, models.QualityMeta{MappingQuality: models.MappingPartial}, fmt.Errorf("no canonical model registered for operation %q", m.cmap.OperationID)
	}
	if descriptor.ModelType != m.cmap.ModelType {
		return nil, models.QualityMeta{MappingQuality: models.MappingPartial}, fmt.Errorf("canonical map model type %q does not match registered model %q for operation %q", m.cmap.ModelType, descriptor.ModelType, m.cmap.OperationID)
	}
	return m.mapGeneric(items, descriptor)
}

// MapXML converts raw NETCONF XML into a generic JSON-like structure, then
// maps it into canonical models via MapJSON. The XML decoder handles:
//   - <rpc-reply><data>...</data></rpc-reply> wrappers (stripped)
//   - Sibling list elements without a parent container (Nokia SRL)
//   - Nested containers (Nokia SROS)
//   - Namespace-tagged elements
func (m *SchemaMapper) MapXML(rawXML []byte) (any, models.QualityMeta, error) {
	generic, err := xmlToGeneric(rawXML)
	if err != nil {
		return nil, models.QualityMeta{MappingQuality: models.MappingPartial}, fmt.Errorf("XML parse failed: %w", err)
	}
	return m.MapJSON(generic)
}

func (m *SchemaMapper) mapGeneric(items []map[string]any, descriptor CanonicalModelDescriptor) (any, models.QualityMeta, error) {
	if descriptor.List {
		out := reflect.MakeSlice(reflect.SliceOf(descriptor.Type), 0, len(items))
		for _, item := range items {
			value, mapped := m.extractGeneric(item, descriptor.Type)
			if !mapped || !hasRequiredFields(value, descriptor.RequiredFields) {
				continue
			}
			out = reflect.Append(out, value)
		}
		if out.Len() == 0 {
			return out.Interface(), models.QualityMeta{MappingQuality: models.MappingPartial, Warnings: []string{"no canonical records extracted"}}, nil
		}
		return out.Interface(), models.QualityMeta{MappingQuality: models.MappingExact}, nil
	}

	merged := make(map[string]any)
	for _, item := range items {
		flat := m.flattenWithAliases(item)
		for k, v := range flat {
			if _, exists := merged[k]; !exists {
				merged[k] = v
			}
		}
	}
	value, mapped := m.extractGeneric(merged, descriptor.Type)
	if !mapped || !hasRequiredFields(value, descriptor.RequiredFields) {
		return value.Interface(), models.QualityMeta{MappingQuality: models.MappingPartial, Warnings: []string{"canonical singleton payload is incomplete"}}, nil
	}
	return value.Interface(), models.QualityMeta{MappingQuality: models.MappingExact}, nil
}

func (m *SchemaMapper) extractGeneric(data map[string]any, modelType reflect.Type) (reflect.Value, bool) {
	flat := m.flattenWithAliases(data)
	value := reflect.New(modelType).Elem()
	mapped := false
	extensions := make(map[string]any)

	for key, val := range flat {
		fm, ok := m.fieldIndex[key]
		if !ok {
			extensions[key] = val
			continue
		}
		if setModelField(value, fm, val) {
			mapped = true
		}
	}
	for _, child := range m.childMappings {
		if setChildMapping(value, data, child) {
			mapped = true
		}
	}

	if len(extensions) > 0 {
		if field := value.FieldByName("VendorExtensions"); field.IsValid() && field.CanSet() && field.Kind() == reflect.Map {
			field.Set(reflect.ValueOf(extensions))
		}
	}
	return value, mapped
}

func setChildMapping(parent reflect.Value, data map[string]any, child ChildMapping) bool {
	field := parent.FieldByName(child.CanonicalField)
	if !field.IsValid() || !field.CanSet() {
		return false
	}
	raw, ok := findNestedValue(data, child.SourceContainer)
	if !ok || raw == nil {
		return false
	}
	modelType, ok := modelTypeByName(child.ModelType)
	if !ok {
		return false
	}
	items := rawToMapSlice(raw)
	if child.List {
		slice := reflect.MakeSlice(reflect.SliceOf(modelType), 0, len(items))
		for _, item := range items {
			v, mapped := mapChildItem(item, modelType, child.Fields)
			if mapped {
				slice = reflect.Append(slice, v)
			}
		}
		if slice.Len() == 0 {
			return false
		}
		field.Set(slice)
		return true
	}
	if len(items) == 0 {
		return false
	}
	v, mapped := mapChildItem(items[0], modelType, child.Fields)
	if !mapped {
		return false
	}
	if field.Kind() == reflect.Pointer {
		ptr := reflect.New(modelType)
		ptr.Elem().Set(v)
		field.Set(ptr)
		return true
	}
	field.Set(v)
	return true
}

func mapChildItem(data map[string]any, modelType reflect.Type, fields []FieldMapping) (reflect.Value, bool) {
	value := reflect.New(modelType).Elem()
	mapped := false
	index := map[string]FieldMapping{}
	for _, f := range fields {
		index[f.YANGLeaf] = f
	}
	flat := flattenKnownContainers(data, map[string]bool{"state": true, "config": true, "counters": true})
	for key, raw := range flat {
		fm, ok := index[key]
		if !ok {
			continue
		}
		if setModelField(value, fm, raw) {
			mapped = true
		}
	}
	return value, mapped
}

func setModelField(value reflect.Value, fm FieldMapping, raw any) bool {
	field := value.FieldByName(fm.CanonicalField)
	if !field.IsValid() || !field.CanSet() {
		return false
	}
	if isZeroNonEmpty(field) {
		return false
	}

	rawString := toString(raw)
	if fm.Normalizer != "" {
		rawString = NormalizeValue(fm.Normalizer, rawString)
		raw = rawString
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(rawString)
		return rawString != ""
	case reflect.Bool:
		b := toBool(raw)
		field.SetBool(b)
		return b
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i := toInt64(raw)
		field.SetInt(i)
		return i != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u := toUint64(raw)
		field.SetUint(u)
		return u != 0
	case reflect.Float32, reflect.Float64:
		f := toFloat64(raw)
		field.SetFloat(f)
		return f != 0
	case reflect.Slice:
		if field.Type().Elem().Kind() == reflect.String {
			items := toStringSlice(raw)
			field.Set(reflect.ValueOf(items))
			return len(items) > 0
		}
		// Struct slices are populated by declarative ChildMappings before flat
		// field assignment reaches this generic scalar conversion path.
		return false
	case reflect.Pointer:
		// Pointer structs are populated by declarative ChildMappings before flat
		// field assignment reaches this generic scalar conversion path.
		return false
	default:
		return false
	}
}

func isZeroNonEmpty(field reflect.Value) bool {
	switch field.Kind() {
	case reflect.String:
		return field.String() != ""
	case reflect.Bool:
		return field.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return field.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return field.Uint() != 0
	case reflect.Float32, reflect.Float64:
		return field.Float() != 0
	case reflect.Slice, reflect.Map:
		return field.Len() != 0
	default:
		return false
	}
}

func hasRequiredFields(value reflect.Value, fields []string) bool {
	for _, name := range fields {
		field := value.FieldByName(name)
		if !field.IsValid() || isReflectZero(field) {
			return false
		}
	}
	return true
}

func isReflectZero(v reflect.Value) bool {
	return reflect.DeepEqual(v.Interface(), reflect.Zero(v.Type()).Interface())
}

// ---------------------------------------------------------------------------
// Interface mapping
// ---------------------------------------------------------------------------

func (m *SchemaMapper) mapInterfaces(items []map[string]any) (any, models.QualityMeta, error) {
	interfaces := make([]models.InterfaceState, 0, len(items))
	for _, item := range items {
		iface := m.extractInterface(item)
		if iface.Name != "" {
			interfaces = append(interfaces, iface)
		}
	}
	quality := qualityForInterfaces(interfaces)
	return interfaces, quality, nil
}

func (m *SchemaMapper) extractInterface(data map[string]any) models.InterfaceState {
	flat := m.flattenWithAliases(data)

	iface := models.InterfaceState{}
	var extensions map[string]any

	for key, val := range flat {
		fm, ok := m.fieldIndex[key]
		if !ok {
			// Unmapped leaf → VendorExtensions
			if extensions == nil {
				extensions = make(map[string]any)
			}
			extensions[key] = val
			continue
		}
		m.setInterfaceField(&iface, fm, val)
	}
	iface.VendorExtensions = extensions
	return iface
}

func (m *SchemaMapper) setInterfaceField(iface *models.InterfaceState, fm FieldMapping, val any) {
	s := toString(val)
	if fm.Normalizer != "" {
		s = NormalizeValue(fm.Normalizer, s)
	}

	switch fm.CanonicalField {
	case "Name":
		if iface.Name == "" {
			iface.Name = s
		}
	case "Type":
		if iface.Type == "" {
			iface.Type = s
		}
	case "AdminStatus":
		if iface.AdminStatus == "" {
			iface.AdminStatus = s
		}
	case "OperStatus":
		if iface.OperStatus == "" {
			iface.OperStatus = s
		}
	case "Description":
		if iface.Description == "" {
			iface.Description = s
		}
	case "MTU":
		if iface.MTU == 0 {
			iface.MTU = toInt(val)
		}
	case "Speed":
		if iface.Speed == "" {
			iface.Speed = s
		}
	}
}

func qualityForInterfaces(ifaces []models.InterfaceState) models.QualityMeta {
	if len(ifaces) == 0 {
		return models.QualityMeta{MappingQuality: models.MappingPartial, Warnings: []string{"no interfaces extracted"}}
	}
	allComplete := true
	for _, i := range ifaces {
		if i.AdminStatus == "" || i.OperStatus == "" {
			allComplete = false
			break
		}
	}
	if allComplete {
		return models.QualityMeta{MappingQuality: models.MappingExact}
	}
	return models.QualityMeta{MappingQuality: models.MappingDerived}
}

// ---------------------------------------------------------------------------
// SystemVersion mapping
// ---------------------------------------------------------------------------

func (m *SchemaMapper) mapSystemVersion(items []map[string]any) (any, models.QualityMeta, error) {
	// SystemVersion is typically a single object, not a list.
	// Merge all items into one flat map (handles multi-path responses like
	// SRL system/information + system/name).
	merged := make(map[string]any)
	for _, item := range items {
		flat := m.flattenWithAliases(item)
		for k, v := range flat {
			if _, exists := merged[k]; !exists {
				merged[k] = v
			}
		}
	}

	sv := models.SystemVersion{}
	var extensions map[string]any

	for key, val := range merged {
		fm, ok := m.fieldIndex[key]
		if !ok {
			if extensions == nil {
				extensions = make(map[string]any)
			}
			extensions[key] = val
			continue
		}
		s := toString(val)
		if fm.Normalizer != "" {
			s = NormalizeValue(fm.Normalizer, s)
		}
		switch fm.CanonicalField {
		case "Hostname":
			if sv.Hostname == "" {
				sv.Hostname = s
			}
		case "SoftwareVersion":
			if sv.SoftwareVersion == "" {
				sv.SoftwareVersion = s
			}
		case "SystemType":
			if sv.SystemType == "" {
				sv.SystemType = s
			}
		case "ChassisType":
			if sv.ChassisType == "" {
				sv.ChassisType = s
			}
		case "Uptime":
			if sv.Uptime == "" {
				sv.Uptime = s
			}
		}
	}
	sv.VendorExtensions = extensions

	quality := models.QualityMeta{MappingQuality: models.MappingExact}
	if sv.Hostname == "" && sv.SoftwareVersion == "" {
		quality.MappingQuality = models.MappingPartial
		quality.Warnings = []string{"no hostname or version extracted"}
	} else if sv.Hostname == "" || sv.SoftwareVersion == "" {
		quality.MappingQuality = models.MappingDerived
	}

	return sv, quality, nil
}

// ---------------------------------------------------------------------------
// Helpers: unwrap, flatten, type coercion
// ---------------------------------------------------------------------------

// unwrapToList normalizes the response into a list of flat objects:
//   - If it's already a []any → convert elements to map[string]any
//   - If it's a map with a vendor-namespaced key whose value is a list → unwrap
//   - If it's a single map → return as single-element list
func (m *SchemaMapper) unwrapToList(data any) []map[string]any {
	switch v := data.(type) {
	case []any:
		return toMapSlice(v)
	case []map[string]any:
		return v
	case map[string]any:
		// Check for vendor-namespaced wrapper: {"srl_nokia-interfaces:interface": [...]}
		for key, val := range v {
			if strings.Contains(key, ":") {
				switch inner := val.(type) {
				case []any:
					return toMapSlice(inner)
				case []map[string]any:
					return inner
				case map[string]any:
					return []map[string]any{inner}
				}
			}
		}
		// No namespaced wrapper — look for known list keys.
		for _, listKey := range []string{
			"interface", "interfaces", "port", "neighbor", "neighbors", "route", "routes",
			"alarm", "alarms", "component", "components", "entry", "entries", "instance",
			"policy-definition", "acl-set", "queue", "classifier", "process", "server",
		} {
			if arr, ok := v[listKey]; ok {
				switch inner := arr.(type) {
				case []any:
					return toMapSlice(inner)
				case []map[string]any:
					return inner
				}
			}
		}
		// Single object (e.g. system version).
		return []map[string]any{v}
	default:
		return nil
	}
}

// flattenWithAliases returns a flat key→value map. If the data contains
// sub-containers listed in aliasSet (e.g. "state", "config"), their leaf
// children are merged into the parent level. First-seen value wins — this
// gives "state" priority over "config" when both are present, as long as
// the YAML order puts state first (JSON maps are unordered, but OpenConfig
// state typically shadows config).
func (m *SchemaMapper) flattenWithAliases(data map[string]any) map[string]any {
	flat := make(map[string]any, len(data))

	// First pass: direct leaves.
	for k, v := range data {
		if _, isAlias := m.aliasSet[k]; isAlias {
			continue // handle in second pass
		}
		if isScalar(v) {
			flat[k] = v
		}
	}

	// Second pass: merge alias containers.
	for k, v := range data {
		if !m.aliasSet[k] {
			continue
		}
		sub, ok := v.(map[string]any)
		if !ok {
			continue
		}
		for sk, sv := range sub {
			if isScalar(sv) {
				if _, exists := flat[sk]; !exists {
					flat[sk] = sv
				}
			}
		}
	}

	return flat
}

func toMapSlice(items []any) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

func rawToMapSlice(raw any) []map[string]any {
	switch v := raw.(type) {
	case []any:
		return toMapSlice(v)
	case []map[string]any:
		return v
	case map[string]any:
		return []map[string]any{v}
	default:
		return nil
	}
}

func findNestedValue(data map[string]any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	parts := strings.Split(path, "/")
	var current any = data
	for _, part := range parts {
		if part == "" {
			continue
		}
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := m[part]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func flattenKnownContainers(data map[string]any, aliases map[string]bool) map[string]any {
	flat := make(map[string]any, len(data))
	for k, v := range data {
		if aliases[k] {
			continue
		}
		if isScalar(v) {
			flat[k] = v
		}
	}
	for k, v := range data {
		if !aliases[k] {
			continue
		}
		if sub, ok := v.(map[string]any); ok {
			for sk, sv := range sub {
				if isScalar(sv) {
					flat[sk] = sv
				}
			}
		}
	}
	return flat
}

func modelTypeByName(name string) (reflect.Type, bool) {
	switch name {
	case "InterfaceCounters":
		return reflect.TypeOf(models.InterfaceCounters{}), true
	case "LACPMember":
		return reflect.TypeOf(models.LACPMember{}), true
	case "ACLEntry":
		return reflect.TypeOf(models.ACLEntry{}), true
	case "ACLInterface":
		return reflect.TypeOf(models.ACLInterface{}), true
	case "QoSQueue":
		return reflect.TypeOf(models.QoSQueue{}), true
	case "QoSScheduler":
		return reflect.TypeOf(models.QoSScheduler{}), true
	case "RoutingPolicyStatement":
		return reflect.TypeOf(models.RoutingPolicyStatement{}), true
	default:
		return nil, false
	}
}

func isScalar(v any) bool {
	switch v.(type) {
	case string, float64, int, int64, uint64, bool, json.Number:
		return true
	default:
		return false
	}
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case json.Number:
		return t.String()
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i)
		}
	case string:
		var i int
		if _, err := fmt.Sscanf(t, "%d", &i); err == nil {
			return i
		}
	}
	return 0
}

func toInt64(v any) int64 {
	switch t := v.(type) {
	case int:
		return int64(t)
	case int64:
		return t
	case float64:
		return int64(t)
	case json.Number:
		i, _ := t.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return i
	default:
		return 0
	}
}

func toUint64(v any) uint64 {
	switch t := v.(type) {
	case uint64:
		return t
	case int:
		if t > 0 {
			return uint64(t)
		}
	case int64:
		if t > 0 {
			return uint64(t)
		}
	case float64:
		if t > 0 {
			return uint64(t)
		}
	case json.Number:
		if u, err := strconv.ParseUint(t.String(), 10, 64); err == nil {
			return u
		}
	case string:
		u, _ := strconv.ParseUint(strings.TrimSpace(t), 10, 64)
		return u
	}
	return 0
}

func toFloat64(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case uint64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		cleaned := strings.TrimSuffix(strings.TrimSpace(t), "%")
		f, _ := strconv.ParseFloat(cleaned, 64)
		return f
	default:
		return 0
	}
}

func toBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true", "up", "active", "enabled", "enable", "yes", "1":
			return true
		default:
			return false
		}
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	default:
		return false
	}
}

func toStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := toString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.Contains(t, ",") {
			parts := strings.Split(t, ",")
			out := make([]string, 0, len(parts))
			for _, p := range parts {
				if s := strings.TrimSpace(p); s != "" {
					out = append(out, s)
				}
			}
			return out
		}
		if strings.TrimSpace(t) == "" {
			return nil
		}
		return []string{strings.TrimSpace(t)}
	default:
		return nil
	}
}

// ---------------------------------------------------------------------------
// XML → generic JSON-like structure
// ---------------------------------------------------------------------------

// xmlToGeneric parses NETCONF XML into a generic map/slice structure that
// MapJSON can process. It handles:
//   - <rpc-reply> and <data> wrappers (stripped automatically)
//   - Sibling elements of the same name (collected into a slice)
//   - Nested containers (recursive)
//   - Leaf text content
func xmlToGeneric(rawXML []byte) (any, error) {
	decoder := xml.NewDecoder(decoder_newReader(rawXML))
	decoder.Strict = false

	result, err := decodeXMLElement(decoder)
	if err != nil {
		return nil, err
	}

	// Strip common NETCONF wrappers.
	return stripNetconfWrappers(result), nil
}

// decodeXMLElement recursively decodes XML into map[string]any.
// Sibling elements with the same local name are combined into []any.
func decodeXMLElement(d *xml.Decoder) (any, error) {
	fields := make(map[string]any)
	var textContent strings.Builder

	for {
		tok, err := d.Token()
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			key := t.Name.Local
			child, cerr := decodeXMLElement(d)
			if cerr != nil {
				return nil, cerr
			}

			if existing, ok := fields[key]; ok {
				// Duplicate key — convert to slice.
				switch ev := existing.(type) {
				case []any:
					fields[key] = append(ev, child)
				default:
					fields[key] = []any{ev, child}
				}
			} else {
				fields[key] = child
			}

		case xml.CharData:
			textContent.Write(t)

		case xml.EndElement:
			// If this element had no children, return the text content.
			if len(fields) == 0 {
				text := strings.TrimSpace(textContent.String())
				if text != "" {
					return text, nil
				}
				return nil, nil
			}
			return fields, nil
		}
	}

	if len(fields) > 0 {
		return fields, nil
	}
	text := strings.TrimSpace(textContent.String())
	if text != "" {
		return text, nil
	}
	return nil, nil
}

// stripNetconfWrappers recursively unwraps single-key container maps that
// are common in NETCONF XML: <rpc-reply>, <data>, synthetic <_root>, and
// vendor-specific intermediate containers like <interfaces>, <state>,
// <router>, <system>. Unwrapping stops when:
//   - the map has more than one key (actual data with siblings)
//   - the single key's value is not a map (it's a list or scalar)
func stripNetconfWrappers(data any) any {
	m, ok := data.(map[string]any)
	if !ok {
		return data
	}

	if len(m) == 1 {
		for _, val := range m {
			if inner, ok := val.(map[string]any); ok {
				return stripNetconfWrappers(inner)
			}
			// Value is a list or scalar — keep the key.
			return data
		}
	}

	return data
}

// decoder_newReader wraps rawXML in a reader suitable for xml.NewDecoder.
// It injects a synthetic root element when the XML contains sibling
// top-level elements (common in NETCONF responses). Non-well-formed XML
// (multiple roots) would otherwise cause a parse error.
func decoder_newReader(rawXML []byte) *strings.Reader {
	trimmed := strings.TrimSpace(string(rawXML))
	// Fast heuristic: if the content has multiple root-level start tags,
	// wrap it. Count occurrences of '<' at depth 0 (not inside a tag).
	if needsWrapping(trimmed) {
		return strings.NewReader("<_root>" + trimmed + "</_root>")
	}
	return strings.NewReader(trimmed)
}

// needsWrapping returns true when the XML fragment has multiple root elements.
func needsWrapping(xmlStr string) bool {
	depth := 0
	roots := 0
	i := 0
	for i < len(xmlStr) {
		if xmlStr[i] == '<' {
			if i+1 < len(xmlStr) && xmlStr[i+1] == '/' {
				// Closing tag.
				depth--
				i = skipPast(xmlStr, i, '>')
				continue
			}
			if i+1 < len(xmlStr) && xmlStr[i+1] == '?' {
				// Processing instruction — skip.
				i = skipPast(xmlStr, i, '>')
				continue
			}
			// Opening tag.
			if depth == 0 {
				roots++
				if roots > 1 {
					return true
				}
			}
			// Check for self-closing.
			end := strings.IndexByte(xmlStr[i:], '>')
			if end < 0 {
				break
			}
			if xmlStr[i+end-1] == '/' {
				// Self-closing: don't increase depth.
				i = i + end + 1
				continue
			}
			depth++
			i = i + end + 1
			continue
		}
		i++
	}
	return false
}

func skipPast(s string, from int, ch byte) int {
	idx := strings.IndexByte(s[from:], ch)
	if idx < 0 {
		return len(s)
	}
	return from + idx + 1
}
