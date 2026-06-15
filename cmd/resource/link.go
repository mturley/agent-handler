package resource

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/worktree"
	"github.com/spf13/cobra"
)

var linkCmd = &cobra.Command{
	Use:   "link",
	Short: "Link two resources in a parent-child relationship",
	RunE:  runLink,
}

var (
	linkChild        string
	linkParent       string
	linkRelationship string
	linkChildURL     string
	linkParentURL    string
	linkSource       string
)

func init() {
	ResourceCmd.AddCommand(linkCmd)
	linkCmd.Flags().StringVar(&linkChild, "child", "", "child resource ID (format: type:id)")
	linkCmd.Flags().StringVar(&linkParent, "parent", "", "parent resource ID (format: type:id)")
	linkCmd.Flags().StringVar(&linkRelationship, "relationship", "", "relationship type (e.g., epic_child)")
	linkCmd.Flags().StringVar(&linkChildURL, "child-url", "", "child resource URL (optional)")
	linkCmd.Flags().StringVar(&linkParentURL, "parent-url", "", "parent resource URL (optional)")
	linkCmd.Flags().StringVar(&linkSource, "source", "manual", "source of the relationship")
	linkCmd.MarkFlagRequired("child")
	linkCmd.MarkFlagRequired("parent")
	linkCmd.MarkFlagRequired("relationship")
}

func runLink(cmd *cobra.Command, args []string) error {
	d, err := db.Open(db.DefaultPath())
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Parse child resource
	childType, childID := worktree.ParseResourceID(linkChild)
	if childType == "" {
		return fmt.Errorf("invalid child resource format (expected type:id): %s", linkChild)
	}

	// Parse parent resource
	parentType, parentID := worktree.ParseResourceID(linkParent)
	if parentType == "" {
		return fmt.Errorf("invalid parent resource format (expected type:id): %s", linkParent)
	}

	// Create relationship
	now := time.Now().UTC().Format(time.RFC3339)
	var childURLPtr, parentURLPtr *string
	if linkChildURL != "" {
		childURLPtr = &linkChildURL
	}
	if linkParentURL != "" {
		parentURLPtr = &linkParentURL
	}

	err = d.LinkResources(db.ResourceRelationship{
		ID:           uuid.New().String(),
		ChildType:    childType,
		ChildID:      childID,
		ChildURL:     childURLPtr,
		ParentType:   parentType,
		ParentID:     parentID,
		ParentURL:    parentURLPtr,
		Relationship: linkRelationship,
		Source:       linkSource,
		CreatedAt:    now,
	})
	if err != nil {
		return fmt.Errorf("failed to link resources: %w", err)
	}

	// Output
	if JSONOutput != nil && *JSONOutput {
		output := map[string]interface{}{
			"child":        linkChild,
			"parent":       linkParent,
			"relationship": linkRelationship,
			"source":       linkSource,
			"status":       "linked",
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("✓ Linked %s → %s (%s)\n", linkChild, linkParent, linkRelationship)
		fmt.Printf("  Source: %s\n", linkSource)
	}

	return nil
}
