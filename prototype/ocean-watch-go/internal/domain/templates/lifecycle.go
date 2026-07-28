package templates

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const marketingSchemaVersion = 5

func Validate(config map[string]any, channel, selector string) (map[string]any, error) {
	channels := []string{"marketing", "qianchuan"}
	if channel != "" {
		if channel != "marketing" && channel != "qianchuan" {
			return nil, configurationError("unsupported template channel", map[string]any{"channel": channel})
		}
		channels = []string{channel}
	}
	rows := make([]any, 0, len(channels))
	for _, current := range channels {
		var row map[string]any
		var err error
		if current == "marketing" {
			row, err = validateMarketing(config, selector)
		} else {
			row, err = validateQianchuan(config, selector)
		}
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	ok := true
	for _, value := range rows {
		if !exactTrue(value.(map[string]any)["valid"]) {
			ok = false
		}
	}
	return map[string]any{
		"ok":       ok,
		"mode":     "template_validation",
		"channels": rows,
	}, nil
}

func validateMarketing(config map[string]any, selector string) (map[string]any, error) {
	errorsList := []any{}
	rawTemplates := config["plan_templates"]
	templates := map[string]any{}
	if pythonTruthy(rawTemplates) {
		var ok bool
		templates, ok = rawTemplates.(map[string]any)
		if !ok {
			errorsList = append(errorsList, "plan_templates must be an object")
			templates = map[string]any{}
		}
	}
	if selector != "" {
		if _, exists := templates[selector]; !exists {
			return nil, configurationError("Marketing template not found", map[string]any{"template": selector})
		}
	}
	names := sortedKeys(templates)
	if selector != "" {
		names = []string{selector}
	}
	rows := make([]any, 0, len(names))
	for _, name := range names {
		rows = append(rows, marketingValidationRow(config, name, templates[name]))
	}
	version, versionError := validationSchemaVersion(config["plan_template_schema_version"], "plan_template_schema_version")
	if versionError != "" {
		errorsList = append(errorsList, versionError)
	}
	defaultErrors := []any{}
	if _, ok := config["default_plan_template"].(map[string]any); !ok {
		defaultErrors = append(defaultErrors, "default_plan_template must be an object")
	}
	defaultSkeleton := map[string]any{
		"template": "default_plan_template",
		"valid":    len(defaultErrors) == 0,
		"errors":   defaultErrors,
	}
	migrationRequired := version != marketingSchemaVersion
	valid := len(errorsList) == 0 && !migrationRequired && exactTrue(defaultSkeleton["valid"])
	for _, value := range rows {
		valid = valid && exactTrue(value.(map[string]any)["valid"])
	}
	return map[string]any{
		"channel":                  "marketing",
		"schema_version":           nullableVersion(version, versionError),
		"supported_schema_version": marketingSchemaVersion,
		"migration_required":       migrationRequired,
		"valid":                    valid,
		"errors":                   errorsList,
		"default_skeletons":        []any{defaultSkeleton},
		"templates":                rows,
	}, nil
}

func marketingValidationRow(config map[string]any, name string, raw any) map[string]any {
	errorsList := []any{}
	template, ok := raw.(map[string]any)
	if !ok {
		errorsList = append(errorsList, "Marketing plan template must be an object")
		return map[string]any{"template": name, "valid": false, "errors": errorsList}
	}
	normalized, err := normalizeMarketingTemplate(config, name, template)
	if err != nil {
		errorsList = append(errorsList, err.Error())
		return map[string]any{"template": name, "valid": false, "errors": errorsList}
	}
	if bindingError := marketingBindingError(mapOrEmpty(normalized["bindings"])); bindingError != nil {
		errorsList = append(errorsList, bindingError)
	}
	if strategyError := marketingMaterialStrategyError(normalized["material_strategy"]); strategyError != "" {
		errorsList = append(errorsList, strategyError)
	}
	if fields := marketingFixedMaterialFields(normalized); len(fields) != 0 {
		errorsList = append(errorsList, "runtime material IDs stored in template: "+strings.Join(fields, ", "))
	}
	version, _ := validationSchemaVersion(config["plan_template_schema_version"], "plan_template_schema_version")
	if version >= 4 {
		canonical := marketingCanonicalName(normalized)
		if name != canonical || normalized["display_name"] != canonical {
			errorsList = append(errorsList, "template name must be "+canonical)
		}
	}
	return map[string]any{
		"template": name,
		"valid":    len(errorsList) == 0,
		"errors":   errorsList,
	}
}

func validateQianchuan(config map[string]any, selector string) (map[string]any, error) {
	errorsList := []any{}
	productTemplates := validationTemplateMapping(config, qianchuanProductTemplatesKey, &errorsList)
	liveTemplates := validationTemplateMapping(config, qianchuanLiveTemplatesKey, &errorsList)
	type selectedTemplate struct {
		kind string
		id   string
	}
	selected := []selectedTemplate{}
	if selector != "" {
		for _, candidate := range []struct {
			kind      string
			templates map[string]any
		}{{"product", productTemplates}, {"live", liveTemplates}} {
			for id, raw := range candidate.templates {
				template, _ := raw.(map[string]any)
				if selector == id || (template != nil && selector == stringValue(template["display_name"])) {
					selected = append(selected, selectedTemplate{candidate.kind, id})
				}
			}
		}
		if len(selected) == 0 {
			return nil, configurationError("Qianchuan template not found", map[string]any{"template": selector})
		}
		if len(selected) > 1 {
			return nil, configurationError("Qianchuan template selector is ambiguous; use template_id", map[string]any{"template": selector})
		}
	} else {
		for _, id := range sortedKeys(productTemplates) {
			selected = append(selected, selectedTemplate{"product", id})
		}
		for _, id := range sortedKeys(liveTemplates) {
			selected = append(selected, selectedTemplate{"live", id})
		}
	}
	rows := make([]any, 0, len(selected))
	for _, item := range selected {
		rowErrors := []any{}
		var template map[string]any
		var err error
		if item.kind == "product" {
			template, err = validateQianchuanProductTemplate(productTemplates[item.id])
		} else {
			template, err = validateQianchuanLiveTemplate(liveTemplates[item.id])
		}
		if err != nil {
			rowErrors = append(rowErrors, err.Error())
		} else if template["template_id"] != item.id {
			rowErrors = append(rowErrors, "template key does not match template_id")
		}
		rows = append(rows, map[string]any{
			"template":      item.id,
			"template_kind": item.kind,
			"valid":         len(rowErrors) == 0,
			"errors":        rowErrors,
		})
	}
	defaultSkeletons := []any{
		qianchuanDefaultValidation(config, qianchuanProductDefaultKey, "product", validateQianchuanProductDefault),
		qianchuanDefaultValidation(config, qianchuanLiveDefaultKey, "live", validateQianchuanLiveDefault),
	}
	productVersion, productVersionError := validationSchemaVersion(config[qianchuanProductSchemaVersionKey], qianchuanProductSchemaVersionKey)
	liveVersion, liveVersionError := validationSchemaVersion(config[qianchuanLiveSchemaVersionKey], qianchuanLiveSchemaVersionKey)
	for _, message := range []string{productVersionError, liveVersionError} {
		if message != "" {
			errorsList = append(errorsList, message)
		}
	}
	migrationRequired := productVersion != qianchuanProductSchemaVersion || liveVersion != qianchuanLiveSchemaVersion
	valid := len(errorsList) == 0 && !migrationRequired
	for _, collection := range [][]any{defaultSkeletons, rows} {
		for _, value := range collection {
			valid = valid && exactTrue(value.(map[string]any)["valid"])
		}
	}
	var selectedKind any
	if selector != "" {
		selectedKind = selected[0].kind
	}
	return map[string]any{
		"channel":                "qianchuan",
		"selected_template_kind": selectedKind,
		"schema_versions": map[string]any{
			"product": nullableVersion(productVersion, productVersionError),
			"live":    nullableVersion(liveVersion, liveVersionError),
		},
		"supported_schema_versions": map[string]any{
			"product": qianchuanProductSchemaVersion,
			"live":    qianchuanLiveSchemaVersion,
		},
		"migration_required": migrationRequired,
		"valid":              valid,
		"errors":             errorsList,
		"default_skeletons":  defaultSkeletons,
		"templates":          rows,
	}, nil
}

func validationTemplateMapping(config map[string]any, key string, errorsList *[]any) map[string]any {
	raw := config[key]
	if !pythonTruthy(raw) {
		return map[string]any{}
	}
	templates, ok := raw.(map[string]any)
	if !ok {
		*errorsList = append(*errorsList, key+" must be an object")
		return map[string]any{}
	}
	return templates
}

func qianchuanDefaultValidation(
	config map[string]any,
	key string,
	kind string,
	validator func(any) error,
) map[string]any {
	errorsList := []any{}
	raw, exists := config[key]
	if !exists || raw == nil {
		errorsList = append(errorsList, key+" is missing")
	} else if err := validator(raw); err != nil {
		errorsList = append(errorsList, err.Error())
	}
	return map[string]any{
		"template":      key,
		"template_kind": kind,
		"valid":         len(errorsList) == 0,
		"errors":        errorsList,
	}
}

func validateQianchuanProductDefault(value any) error {
	template, ok := value.(map[string]any)
	if !ok {
		return configurationError("Qianchuan product default template must be an object", nil)
	}
	if template["template_type"] != qianchuanProductTemplateType {
		return configurationError("invalid Qianchuan product default template type", nil)
	}
	if !exactFalse(template["business_usable"]) {
		return configurationError("Qianchuan product default template cannot be business usable", nil)
	}
	bindings, ok := template["bindings"].(map[string]any)
	if !ok || bindings["channel"] != "qianchuan" {
		return configurationError("Qianchuan product default template channel must be qianchuan", nil)
	}
	if !isMissing(bindings["advertiser_id"]) {
		return configurationError("Qianchuan product default template must not bind an advertiser", nil)
	}
	if !isMissing(bindings["product_name"]) {
		return configurationError("Qianchuan product default template must not bind a product name", nil)
	}
	if ids := bindings["product_ids"]; ids != nil && !reflect.DeepEqual(ids, []any{}) {
		return configurationError("Qianchuan product default template must not bind products", nil)
	}
	if _, err := validateQianchuanProductDelivery(template["delivery_setting"]); err != nil {
		return err
	}
	strategy, ok := template["material_strategy"].(map[string]any)
	if !ok || len(strategy) != 2 || strategy["source_type"] != qianchuanProductMaterialSource || !exactFalse(strategy["persist_material_ids"]) {
		return configurationError("invalid Qianchuan product default material strategy", nil)
	}
	if forbidden := qianchuanForbiddenKey(template); forbidden != "" {
		return configurationError("Qianchuan product default template contains runtime fields", map[string]any{"field": forbidden})
	}
	return nil
}

func validateQianchuanLiveDefault(value any) error {
	template, ok := value.(map[string]any)
	if !ok {
		return configurationError("Qianchuan live default template must be an object", nil)
	}
	if template["template_type"] != qianchuanLiveTemplateType {
		return configurationError("invalid Qianchuan live default template type", nil)
	}
	if !exactFalse(template["business_usable"]) {
		return configurationError("Qianchuan live default template cannot be business usable", nil)
	}
	bindings, ok := template["bindings"].(map[string]any)
	if !ok || bindings["channel"] != "qianchuan" {
		return configurationError("Qianchuan live default template channel must be qianchuan", nil)
	}
	for _, field := range []string{"advertiser_id", "creator_name", "aweme_id"} {
		if !isMissing(bindings[field]) {
			return configurationError("Qianchuan live default template must not bind "+field, nil)
		}
	}
	if _, err := validateQianchuanLiveDelivery(template["delivery_setting"]); err != nil {
		return err
	}
	if _, err := validateQianchuanLiveCreative(template["creative_setting"]); err != nil {
		return err
	}
	strategy, ok := template["material_strategy"].(map[string]any)
	if !ok || len(strategy) != 2 || strategy["source_type"] != qianchuanLiveMaterialSource || !exactFalse(strategy["persist_material_ids"]) {
		return configurationError("invalid Qianchuan live default material strategy", nil)
	}
	return nil
}

func Delete(config map[string]any, channel, selector string, force bool) (map[string]any, map[string]any, error) {
	updated := cloneMap(config)
	if channel == "marketing" {
		version, _ := validationSchemaVersion(config["plan_template_schema_version"], "plan_template_schema_version")
		if version != marketingSchemaVersion {
			return nil, nil, configurationError("migrate Marketing templates before deletion", nil)
		}
		rawTemplates := config["plan_templates"]
		templates, ok := rawTemplates.(map[string]any)
		if !ok && pythonTruthy(rawTemplates) {
			return nil, nil, configurationError("plan_templates must be an object", nil)
		}
		if templates == nil {
			templates = map[string]any{}
		}
		raw, exists := templates[selector]
		if !exists {
			return nil, nil, configurationError("Marketing template not found", map[string]any{"template": selector})
		}
		validation := marketingValidationRow(config, selector, raw)
		if !exactTrue(validation["valid"]) {
			return nil, nil, configurationError("validate the Marketing template before deletion", map[string]any{
				"template": selector,
				"errors":   validation["errors"],
			})
		}
		dependents, err := marketingDependents(config, selector)
		if err != nil {
			return nil, nil, err
		}
		if len(dependents) != 0 && !force {
			return nil, nil, configurationError("template is referenced by other templates; pass --force to delete", map[string]any{
				"dependents": dependents,
			})
		}
		clonedTemplates := cloneMap(templates)
		delete(clonedTemplates, selector)
		updated["plan_templates"] = clonedTemplates
		return updated, map[string]any{
			"channel":    "marketing",
			"template":   selector,
			"dependents": dependents,
		}, nil
	}
	if channel != "qianchuan" {
		return nil, nil, configurationError("unsupported template channel", map[string]any{"channel": channel})
	}
	productConfig, err := ensureQianchuanProductConfig(config)
	if err != nil {
		return nil, nil, err
	}
	liveConfig, err := ensureQianchuanLiveConfig(config)
	if err != nil {
		return nil, nil, err
	}
	productVersion, _ := validationSchemaVersion(config[qianchuanProductSchemaVersionKey], qianchuanProductSchemaVersionKey)
	liveVersion, _ := validationSchemaVersion(config[qianchuanLiveSchemaVersionKey], qianchuanLiveSchemaVersionKey)
	if productVersion != qianchuanProductSchemaVersion {
		return nil, nil, configurationError("migrate Qianchuan templates before deletion", nil)
	}
	if liveVersion != qianchuanLiveSchemaVersion {
		return nil, nil, configurationError("migrate Qianchuan live templates before deletion", nil)
	}
	template, productErr := resolveQianchuanProductTemplate(productConfig, selector)
	templateKind := "product"
	if productErr == nil {
		templates := cloneMap(mapOrEmpty(productConfig[qianchuanProductTemplatesKey]))
		delete(templates, stringValue(template["template_id"]))
		productConfig[qianchuanProductTemplatesKey] = templates
		updated = productConfig
	} else {
		template, err = resolveQianchuanLiveTemplate(liveConfig, selector)
		if err != nil {
			return nil, nil, productErr
		}
		templates := cloneMap(mapOrEmpty(liveConfig[qianchuanLiveTemplatesKey]))
		delete(templates, stringValue(template["template_id"]))
		liveConfig[qianchuanLiveTemplatesKey] = templates
		updated = liveConfig
		templateKind = "live"
	}
	return updated, map[string]any{
		"channel":       "qianchuan",
		"template":      template["template_id"],
		"name":          template["display_name"],
		"template_kind": templateKind,
		"dependents":    []any{},
	}, nil
}

func marketingDependents(config map[string]any, selector string) ([]any, error) {
	templates := mapOrEmpty(config["plan_templates"])
	dependents := []any{}
	for _, name := range sortedKeys(templates) {
		if name == selector {
			continue
		}
		raw, ok := templates[name].(map[string]any)
		if !ok {
			return nil, configurationError("a dependent Marketing template is invalid; validate templates before deletion", map[string]any{"template": name})
		}
		normalized, err := normalizeMarketingTemplate(config, name, raw)
		if err != nil {
			return nil, configurationError("a dependent Marketing template is invalid; validate templates before deletion", map[string]any{"template": name})
		}
		references := []any{}
		if mapOrEmpty(normalized["created_from"])["template"] == selector {
			references = append(references, "created_from.template")
		}
		if mapOrEmpty(normalized["copy_materials"])["copied_from_template"] == selector {
			references = append(references, "copy_materials.copied_from_template")
		}
		if len(references) != 0 {
			dependents = append(dependents, map[string]any{"template": name, "references": references})
		}
	}
	return dependents, nil
}

func SetCopy(config map[string]any, templateName string, titles []string, fromTemplate string) (map[string]any, error) {
	updated := cloneMap(config)
	templates := mapOrEmpty(updated["plan_templates"])
	raw, exists := templates[templateName]
	if !exists {
		return nil, configurationError("plan template not found: "+templateName, nil)
	}
	version, err := parseVersion(updated["plan_template_schema_version"], 1, "plan_template_schema_version")
	if err != nil {
		return nil, err
	}
	if version < marketingSchemaVersion {
		return nil, configurationError("migrate the config before setting copy materials", nil)
	}
	if (len(titles) != 0) == (fromTemplate != "") {
		return nil, configurationError("provide either titles or one source template", nil)
	}
	template, ok := raw.(map[string]any)
	if !ok {
		return nil, configurationError("Marketing plan template must be an object", nil)
	}
	copyMaterials := map[string]any{}
	if fromTemplate != "" {
		sourceRaw, sourceExists := templates[fromTemplate]
		if !sourceExists {
			return nil, configurationError("source plan template not found: "+fromTemplate, nil)
		}
		source, sourceOK := sourceRaw.(map[string]any)
		if !sourceOK {
			return nil, configurationError("Marketing plan template must be an object", nil)
		}
		normalized, normalizeErr := normalizeMarketingTemplate(updated, fromTemplate, source)
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		normalizedTitles, titleErr := normalizeCopyTitles(anyStringList(listOrEmpty(mapOrEmpty(normalized["copy_materials"])["titles"])))
		if titleErr != nil {
			return nil, titleErr
		}
		copyMaterials["titles"] = stringsToAny(normalizedTitles)
		copyMaterials["copied_from_template"] = fromTemplate
	} else {
		normalizedTitles, titleErr := normalizeCopyTitles(titles)
		if titleErr != nil {
			return nil, titleErr
		}
		copyMaterials["titles"] = stringsToAny(normalizedTitles)
	}
	clonedTemplate := cloneMap(template)
	clonedTemplate["copy_materials"] = copyMaterials
	if overrides, ok := clonedTemplate["overrides"].(map[string]any); ok {
		delete(overrides, "titles")
	}
	templates[templateName] = clonedTemplate
	updated["plan_templates"] = templates
	return updated, nil
}

func MigrateMarketing(config map[string]any, confirmRemoveLegacyMaterials bool) (map[string]any, *LegacyMaterialError, error) {
	version, versionError := validationSchemaVersion(config["plan_template_schema_version"], "plan_template_schema_version")
	if versionError != "" {
		return nil, nil, configurationError(versionError, nil)
	}
	if version > marketingSchemaVersion {
		return nil, nil, configurationError(fmt.Sprintf("plan template schema %d is newer than supported %d", version, marketingSchemaVersion), nil)
	}
	if version == marketingSchemaVersion {
		validated := cloneMap(config)
		if _, exists := validated["active_plan_template"]; exists {
			return nil, nil, configurationError("schema v5 does not support active_plan_template; migrate the config", nil)
		}
		for _, name := range sortedKeys(mapOrEmpty(validated["plan_templates"])) {
			row := marketingValidationRow(validated, name, mapOrEmpty(validated["plan_templates"])[name])
			if !exactTrue(row["valid"]) {
				errorsList := listOrEmpty(row["errors"])
				message := "invalid plan template " + name
				if len(errorsList) != 0 {
					message += ": " + stringValue(errorsList[0])
				}
				return nil, nil, configurationError(message, nil)
			}
		}
		return validated, nil, nil
	}
	if version == 4 {
		migrated := cloneMap(config)
		delete(migrated, "active_plan_template")
		migrated["plan_template_schema_version"] = marketingSchemaVersion
		return MigrateMarketingValue(migrated, confirmRemoveLegacyMaterials)
	}
	if version >= 3 {
		migrated, err := migrateMarketingNames(config)
		if err != nil {
			return nil, nil, err
		}
		return MigrateMarketingValue(migrated, confirmRemoveLegacyMaterials)
	}
	return migrateLegacyMarketing(config, confirmRemoveLegacyMaterials)
}

type LegacyMaterialError struct {
	Templates []string
}

func (err *LegacyMaterialError) Error() string {
	return "legacy templates contain fixed video IDs; rerun migration with explicit confirmation: " + strings.Join(err.Templates, ", ")
}

func MigrateMarketingValue(config map[string]any, confirmRemoveLegacyMaterials bool) (map[string]any, *LegacyMaterialError, error) {
	return MigrateMarketing(config, confirmRemoveLegacyMaterials)
}

func migrateMarketingNames(config map[string]any) (map[string]any, error) {
	templates := mapOrEmpty(config["plan_templates"])
	nameMap := map[string]string{}
	owners := map[string]string{}
	for _, oldName := range sortedKeys(templates) {
		raw, ok := templates[oldName].(map[string]any)
		if !ok {
			return nil, configurationError("Marketing plan template must be an object", map[string]any{"template": oldName})
		}
		normalized, err := normalizeMarketingTemplate(config, oldName, raw)
		if err != nil {
			return nil, err
		}
		newName := marketingCanonicalName(normalized)
		if owner, exists := owners[newName]; exists && owner != oldName {
			return nil, configurationError("plan template naming collision after schema v4 migration: "+owner+", "+oldName+" -> "+newName, nil)
		}
		nameMap[oldName] = newName
		owners[newName] = oldName
	}
	migrated := cloneMap(config)
	renamed := map[string]any{}
	for _, oldName := range sortedKeys(templates) {
		newName := nameMap[oldName]
		template := cloneMap(mapOrEmpty(templates[oldName]))
		template["display_name"] = newName
		if createdFrom, ok := template["created_from"].(map[string]any); ok {
			if replacement, exists := nameMap[stringValue(createdFrom["template"])]; exists {
				createdFrom["template"] = replacement
			}
		}
		if copyMaterials, ok := template["copy_materials"].(map[string]any); ok {
			if replacement, exists := nameMap[stringValue(copyMaterials["copied_from_template"])]; exists {
				copyMaterials["copied_from_template"] = replacement
			}
		}
		renamed[newName] = template
	}
	migrated["plan_templates"] = renamed
	migrated["plan_template_schema_version"] = 4
	return migrated, nil
}

func migrateLegacyMarketing(config map[string]any, confirm bool) (map[string]any, *LegacyMaterialError, error) {
	// The legacy transformation is intentionally expressed in terms of the same
	// normalized templates used by queries so unknown root and template fields survive.
	originalBase := marketingDefaultBundle(config)
	base := sharedMarketingDefaultBundle(originalBase)
	templates := map[string]any{}
	affected := []string{}
	for _, name := range sortedKeys(mapOrEmpty(config["plan_templates"])) {
		raw := mapOrEmpty(mapOrEmpty(config["plan_templates"])[name])
		normalized, err := normalizeMarketingTemplate(config, name, raw)
		if err != nil {
			return nil, nil, err
		}
		bindings := cloneMap(mapOrEmpty(normalized["bindings"]))
		if _, exists := bindings["channel"]; !exists {
			bindings["channel"] = "marketing"
		}
		effective := cloneMap(originalBase)
		for _, section := range marketingTemplateSections {
			if override, exists := mapOrEmpty(normalized["overrides"])[section]; exists {
				effective[section] = deepMerge(mapOrEmpty(effective[section]), mapOrEmpty(override))
			}
		}
		effective["titles"] = clone(listOrEmpty(mapOrEmpty(normalized["copy_materials"])["titles"]))
		materials := cloneMap(mapOrEmpty(effective["materials"]))
		for _, field := range []string{"video_ids", "video_cover_ids"} {
			if pythonTruthy(materials[field]) {
				affected = append(affected, name)
			}
			delete(materials, field)
		}
		effective["materials"] = materials
		overrides := deepDifference(base, effective)
		delete(overrides, "titles")
		maximum := mapOrEmpty(effective["defaults"])["max_videos_per_project"]
		if !pythonTruthy(maximum) {
			maximum = 5
		}
		parsedMaximum, err := pythonInteger(maximum)
		if err != nil {
			return nil, nil, configurationError("max_videos_per_project must be an integer", nil)
		}
		result := map[string]any{
			"display_name":   normalized["display_name"],
			"bindings":       bindings,
			"copy_materials": clone(mapOrEmpty(normalized["copy_materials"])),
			"material_strategy": map[string]any{
				"source_type": "ACCOUNT_UPLOAD", "selection_mode": "MANUAL", "max_materials_per_unit": parsedMaximum,
			},
			"overrides": overrides,
		}
		if normalized["created_from"] != nil {
			result["created_from"] = clone(normalized["created_from"])
		}
		for key, value := range raw {
			if !marketingMigrationCoreFields[key] {
				result[key] = clone(value)
			}
		}
		templates[name] = result
	}
	affected = uniqueSorted(affected)
	if len(affected) != 0 && !confirm {
		legacyErr := &LegacyMaterialError{Templates: affected}
		return nil, legacyErr, legacyErr
	}
	migrated := cloneMap(config)
	migrated["plan_template_schema_version"] = 3
	migrated["default_plan_template"] = base
	migrated["plan_templates"] = templates
	for _, section := range marketingTemplateSections {
		delete(migrated, section)
	}
	delete(migrated, "titles")
	return MigrateMarketingValue(migrated, confirm)
}

var marketingMigrationCoreFields = map[string]bool{
	"display_name": true, "bindings": true, "copy_materials": true, "material_strategy": true,
	"created_from": true, "overrides": true, "platform": true, "traffic_source": true,
	"product_label": true, "product_id": true, "defaults": true, "materials": true,
	"resolved_ids": true, "links": true, "tracking_urls": true, "titles": true,
}

func sharedMarketingDefaultBundle(bundle map[string]any) map[string]any {
	shared := cloneMap(bundle)
	defaults := cloneMap(mapOrEmpty(shared["defaults"]))
	for _, field := range []string{"product_name", "product_id", "source"} {
		delete(defaults, field)
	}
	shared["defaults"] = defaults
	resolved := mapOrEmpty(shared["resolved_ids"])
	sharedResolved := map[string]any{}
	for _, field := range []string{"city_ids", "city_names"} {
		if value, exists := resolved[field]; exists {
			sharedResolved[field] = clone(value)
		}
	}
	shared["resolved_ids"] = sharedResolved
	shared["materials"] = map[string]any{}
	shared["links"] = map[string]any{}
	shared["tracking_urls"] = map[string]any{}
	shared["titles"] = []any{}
	return shared
}

func deepDifference(base, value any) map[string]any {
	baseMap, baseOK := base.(map[string]any)
	valueMap, valueOK := value.(map[string]any)
	if !baseOK || !valueOK {
		return map[string]any{}
	}
	result := map[string]any{}
	for key, item := range valueMap {
		baseItem, exists := baseMap[key]
		if !exists {
			result[key] = clone(item)
			continue
		}
		baseNested, baseNestedOK := baseItem.(map[string]any)
		valueNested, valueNestedOK := item.(map[string]any)
		if baseNestedOK && valueNestedOK {
			difference := deepDifference(baseNested, valueNested)
			if len(difference) != 0 {
				result[key] = difference
			}
		} else if !reflect.DeepEqual(normalizeComparable(baseItem), normalizeComparable(item)) {
			result[key] = clone(item)
		}
	}
	return result
}

func ListQianchuanProduct(config map[string]any) (map[string]any, error) {
	templates, normalized, err := listQianchuanProductTemplates(config)
	if err != nil {
		return nil, err
	}
	rows := make([]any, 0, len(templates))
	for _, template := range templates {
		bindings := mapOrEmpty(template["bindings"])
		ids := listOrEmpty(bindings["product_ids"])
		rows = append(rows, map[string]any{
			"template_id": template["template_id"], "name": template["display_name"], "status": template["status"],
			"advertiser_id": bindings["advertiser_id"], "product_name": bindings["product_name"],
			"product_ids": clone(ids), "product_count": len(ids), "material_source_type": qianchuanProductMaterialSource,
		})
	}
	return map[string]any{
		"default_template": map[string]any{
			"name": qianchuanProductDefaultKey, "business_usable": false,
			"template": clone(normalized[qianchuanProductDefaultKey]),
		},
		"templates": rows,
	}, nil
}

func MarketingLifecycleResult(config map[string]any, path, command string, changed bool) (map[string]any, error) {
	templates, err := listMarketingTemplates(config)
	if err != nil {
		return nil, err
	}
	rows := make([]any, 0, len(templates))
	for _, template := range templates {
		rows = append(rows, template)
	}
	return map[string]any{
		"config":           path,
		"command":          command,
		"changed":          changed,
		"created_template": nil,
		"wizard_result":    nil,
		"default_template": marketingDefaultTemplateSummary(config),
		"templates":        rows,
	}, nil
}

func MigrateQianchuanProduct(config map[string]any) (map[string]any, map[string]any, bool, error) {
	normalized, err := ensureQianchuanProductConfig(config)
	if err != nil {
		return nil, nil, false, err
	}
	listed, err := ListQianchuanProduct(normalized)
	if err != nil {
		return nil, nil, false, err
	}
	return normalized, map[string]any{
		"migrated":       !semanticEqual(config, normalized),
		"schema_version": qianchuanProductSchemaVersion,
		"templates":      listed["templates"],
	}, !semanticEqual(config, normalized), nil
}

func ListQianchuanLive(config map[string]any) (map[string]any, error) {
	templates, normalized, err := listQianchuanLiveTemplates(config)
	if err != nil {
		return nil, err
	}
	rows := make([]any, 0, len(templates))
	for _, template := range templates {
		bindings := mapOrEmpty(template["bindings"])
		rows = append(rows, map[string]any{
			"template_id": template["template_id"], "name": template["display_name"], "status": template["status"],
			"advertiser_id": bindings["advertiser_id"], "creator_name": bindings["creator_name"],
			"aweme_id": bindings["aweme_id"], "template_type": "直播全域",
		})
	}
	return map[string]any{
		"default_template": map[string]any{
			"name": qianchuanLiveDefaultKey, "business_usable": false,
			"template": clone(normalized[qianchuanLiveDefaultKey]),
		},
		"templates": rows,
	}, nil
}

func MigrateQianchuanLive(config map[string]any) (map[string]any, map[string]any, bool, error) {
	normalized, err := ensureQianchuanLiveConfig(config)
	if err != nil {
		return nil, nil, false, err
	}
	listed, err := ListQianchuanLive(normalized)
	if err != nil {
		return nil, nil, false, err
	}
	changed := !semanticEqual(config, normalized)
	return normalized, map[string]any{
		"migrated": changed, "schema_version": qianchuanLiveSchemaVersion, "templates": listed["templates"],
	}, changed, nil
}

func marketingMaterialStrategyError(value any) string {
	strategy, ok := value.(map[string]any)
	if !ok {
		return "template material_strategy must be an object"
	}
	sourceType := strategy["source_type"]
	if sourceType != "ACCOUNT_UPLOAD" && sourceType != "CREATOR_AUTHORIZED" {
		return "template material_strategy.source_type must be ACCOUNT_UPLOAD or CREATOR_AUTHORIZED"
	}
	selectionMode := strategy["selection_mode"]
	if selectionMode != "MANUAL" && selectionMode != "LATEST" {
		return "template material_strategy.selection_mode must be MANUAL or LATEST"
	}
	if sourceType == "CREATOR_AUTHORIZED" {
		if maximum := strategy["max_materials_per_unit"]; maximum != nil {
			parsed, err := pythonInteger(maximum)
			if err != nil || parsed < 1 {
				return "creator material max_materials_per_unit must be a positive integer or null"
			}
		}
		filters, ok := strategy["creator_filters"].(map[string]any)
		if !ok {
			return "creator material template requires material_strategy.creator_filters"
		}
		if filters["authorization_status"] != "VALID" {
			return "creator material template requires authorization_status VALID"
		}
		remaining, err := pythonInteger(filters["minimum_remaining_days"])
		if err != nil {
			return "creator material minimum_remaining_days must be an integer"
		}
		if remaining < 0 {
			return "creator material minimum_remaining_days must be non-negative"
		}
		creatorIDs, ok := filters["creator_ids"].([]any)
		if filters["creator_ids"] != nil && !ok {
			return "creator material creator_ids must be decimal string values"
		}
		for _, id := range creatorIDs {
			if !allDigits(stringValue(id)) {
				return "creator material creator_ids must be decimal string values"
			}
		}
		if !reflect.DeepEqual(filters["auth_types"], []any{"VIDEO_ITEM"}) {
			return "creator material auth_types currently supports only VIDEO_ITEM"
		}
		return ""
	}
	maximum, err := pythonInteger(strategy["max_materials_per_unit"])
	if err != nil {
		return "account-upload max_materials_per_unit must be an integer"
	}
	if maximum < 1 {
		return "account-upload max_materials_per_unit must be positive"
	}
	if pythonTruthy(strategy["creator_filters"]) {
		return "account-upload material strategy must not contain creator_filters"
	}
	return ""
}

func marketingFixedMaterialFields(template map[string]any) []string {
	materials, ok := mapOrEmpty(template["overrides"])["materials"].(map[string]any)
	if !ok {
		return nil
	}
	fields := []string{}
	for _, field := range []string{"video_ids", "video_cover_ids"} {
		if _, exists := materials[field]; exists {
			fields = append(fields, "overrides.materials."+field)
		}
	}
	return fields
}

func marketingCanonicalName(template map[string]any) string {
	bindings := mapOrEmpty(template["bindings"])
	templateType := "原生素材"
	if mapOrEmpty(template["material_strategy"])["source_type"] == "ACCOUNT_UPLOAD" {
		templateType = "混剪素材"
	}
	return strings.Join([]string{
		"巨量营销", stringValue(bindings["advertiser_id"]), stringValue(bindings["product_name"]),
		stringValue(bindings["product_id"]), templateType,
	}, "-")
}

func normalizeCopyTitles(titles []string) ([]string, error) {
	result := []string{}
	seen := map[string]bool{}
	for _, raw := range titles {
		value := strings.TrimSpace(raw)
		if value == "" || seen[value] {
			continue
		}
		length := utf8.RuneCountInString(value)
		if length < 5 || length > 30 {
			return nil, configurationError(fmt.Sprintf("copy title length must be 5-30 characters: %s", value), nil)
		}
		seen[value] = true
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil, configurationError("at least one non-empty copy title is required", nil)
	}
	return result, nil
}

func validationSchemaVersion(value any, key string) (int, string) {
	if value == nil || value == "" {
		value = 1
	}
	if _, ok := value.(bool); ok {
		return 0, key + " must be a positive integer"
	}
	parsed, err := pythonInteger(value)
	if err != nil {
		return 0, key + " must be an integer"
	}
	if parsed < 1 {
		return 0, key + " must be a positive integer"
	}
	return parsed, ""
}

func nullableVersion(version int, message string) any {
	if message != "" {
		return nil
	}
	return version
}

func pythonInteger(value any) (int, error) {
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		return int(typed), nil
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed), nil
		}
		parsed, err := typed.Float64()
		return int(parsed), err
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		return parsed, err
	default:
		return 0, fmt.Errorf("not an integer")
	}
}

func anyStringList(values []any) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = stringValue(value)
	}
	return result
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func semanticEqual(left, right any) bool {
	return reflect.DeepEqual(normalizeComparable(left), normalizeComparable(right))
}

func normalizeComparable(value any) any {
	payload, err := json.Marshal(value)
	if err != nil {
		return value
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return value
	}
	return normalized
}
