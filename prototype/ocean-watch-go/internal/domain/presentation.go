package domain

import (
	"fmt"
	"strings"
)

type PresentationColumn struct {
	Field string `json:"field"`
	Label string `json:"label"`
}

type Presentation struct {
	Format                string               `json:"format"`
	Required              bool                 `json:"required"`
	AllowColumnOmission   bool                 `json:"allow_column_omission"`
	AllowColumnReordering bool                 `json:"allow_column_reordering"`
	Columns               []PresentationColumn `json:"columns"`
	RequiredSections      []string             `json:"required_sections,omitempty"`
	Rows                  []map[string]any     `json:"rows,omitempty"`
	RequiredDetails       []PresentationColumn `json:"required_details,omitempty"`
	DetailsOutsideTable   []string             `json:"details_outside_table,omitempty"`
	RenderedMarkdown      string               `json:"rendered_markdown"`
}

func EscapeMarkdownValue(value any) string {
	if value == nil || fmt.Sprint(value) == "" {
		return "—"
	}
	return strings.NewReplacer("|", `\|`, "\r", " ", "\n", " ").Replace(fmt.Sprint(value))
}

func RenderMarkdownTable(columns []PresentationColumn, rows []map[string]any) string {
	labels := make([]string, 0, len(columns))
	separators := make([]string, 0, len(columns))
	for _, column := range columns {
		labels = append(labels, column.Label)
		separators = append(separators, "---")
	}
	lines := []string{
		"| " + strings.Join(labels, " | ") + " |",
		"| " + strings.Join(separators, " | ") + " |",
	}
	for _, row := range rows {
		values := make([]string, 0, len(columns))
		for _, column := range columns {
			values = append(values, EscapeMarkdownValue(row[column.Field]))
		}
		lines = append(lines, "| "+strings.Join(values, " | ")+" |")
	}
	return strings.Join(lines, "\n")
}

var QianchuanBatchColumns = []PresentationColumn{
	{Field: "plan_id", Label: "计划ID"},
	{Field: "creator_nickname", Label: "达人昵称"},
	{Field: "product_id", Label: "商品ID"},
	{Field: "material_id", Label: "素材ID"},
	{Field: "material_title", Label: "素材标题"},
}

func NewQianchuanBatchPresentation(rows []map[string]any, details []string) Presentation {
	return Presentation{
		Format:                "markdown",
		Required:              true,
		AllowColumnOmission:   false,
		AllowColumnReordering: false,
		Columns:               QianchuanBatchColumns,
		Rows:                  rows,
		RequiredDetails: []PresentationColumn{
			{Field: "skipped", Label: "跳过详情"},
			{Field: "query_failures", Label: "查询失败"},
			{Field: "failed_results", Label: "执行失败"},
		},
		DetailsOutsideTable: details,
		RenderedMarkdown:    RenderMarkdownTable(QianchuanBatchColumns, rows),
	}
}
