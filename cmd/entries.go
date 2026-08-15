package cmd

import (
	"fmt"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

const entriesInstruction = "Use these IDs for unattended store maintenance; " +
	"read an entry only when the maintenance decision requires its body."

type entriesOutput struct {
	Instruction string        `json:"instruction"`
	Type        string        `json:"type"`
	Entries     []entriesItem `json:"entries"`
}

type entriesItem struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

var entriesCmd = &cobra.Command{
	Use:   "entries",
	Short: "Query entries by persisted content classification",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		entryType, _ := cmd.Flags().GetString("type")
		switch entryType {
		case cairn.EntryTypeKnowledge, cairn.EntryTypeRemediation, cairn.EntryTypePolicy, "unclassified":
		case "":
			return emitModelError(cmd, classifiedErr(CategoryInvalidInput, "", fmt.Errorf("--type is required")))
		default:
			return emitModelError(cmd, classifiedErr(CategoryInvalidInput, entryType,
				fmt.Errorf("invalid --type %q: use knowledge, remediation, policy, or unclassified", entryType)))
		}
		entries, err := cairn.EntriesByType(cmd.Context(), storePath(), entryType)
		if err != nil {
			return emitModelError(cmd, err)
		}
		items := make([]entriesItem, 0, len(entries))
		for _, e := range entries {
			items = append(items, entriesItem{ID: e.ID, Type: e.Type})
		}
		return emitModelJSON(cmd, entriesOutput{Instruction: entriesInstruction, Type: entryType, Entries: items})
	},
}

func init() {
	entriesCmd.Flags().String("type", "", "classification to query: knowledge|remediation|policy|unclassified")
	rootCmd.AddCommand(entriesCmd)
}
