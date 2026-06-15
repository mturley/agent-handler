package worktree

import (
	"bufio"
	"os"
	"strings"
)

// Resource represents a resource with an ID and URL
type Resource struct {
	ID  string
	URL string
}

// ParseResourceID splits a resource ID on the first colon
func ParseResourceID(resourceID string) (resourceType, id string) {
	parts := strings.SplitN(resourceID, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", resourceID
}

// ReadResources reads resources from a .worktree-resources file
func ReadResources(path string) ([]Resource, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var resources []Resource
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Split on first whitespace
		parts := strings.Fields(line)
		if len(parts) < 2 {
			// Malformed line, skip
			continue
		}

		resources = append(resources, Resource{
			ID:  parts[0],
			URL: parts[1],
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return resources, nil
}

// AppendResource appends a resource to the file, checking for duplicates
func AppendResource(path, resourceID, url string) error {
	// Read existing resources
	existing, err := ReadResources(path)
	if err != nil {
		return err
	}

	// Check for duplicates
	for _, r := range existing {
		if r.ID == resourceID {
			// Already exists, don't append
			return nil
		}
	}

	// Append to file
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(resourceID + " " + url + "\n")
	return err
}

// RemoveResource removes a resource from the file
func RemoveResource(path, resourceID string) error {
	// Read all resources
	resources, err := ReadResources(path)
	if err != nil {
		return err
	}

	// Filter out the resource to remove
	var filtered []Resource
	for _, r := range resources {
		if r.ID != resourceID {
			filtered = append(filtered, r)
		}
	}

	// Rewrite the file
	file, err := os.OpenFile(path, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, r := range filtered {
		_, err = file.WriteString(r.ID + " " + r.URL + "\n")
		if err != nil {
			return err
		}
	}

	return nil
}
