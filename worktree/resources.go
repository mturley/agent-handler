package worktree

import (
	"bufio"
	"os"
	"strings"
)

type Resource struct {
	ID      string
	URL     string
	Primary bool
}

func ParseResourceID(resourceID string) (resourceType, id string) {
	parts := strings.SplitN(resourceID, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", resourceID
}

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

		primary := true
		if strings.HasPrefix(line, "~ ") {
			primary = false
			line = strings.TrimPrefix(line, "~ ")
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		resources = append(resources, Resource{
			ID:      parts[0],
			URL:     parts[1],
			Primary: primary,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return resources, nil
}

func AppendResource(path, resourceID, url string, primary bool) error {
	existing, err := ReadResources(path)
	if err != nil {
		return err
	}

	for _, r := range existing {
		if r.ID == resourceID {
			return nil
		}
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	line := resourceID + " " + url + "\n"
	if !primary {
		line = "~ " + line
	}
	_, err = file.WriteString(line)
	return err
}

func RemoveResource(path, resourceID string) error {
	resources, err := ReadResources(path)
	if err != nil {
		return err
	}

	var filtered []Resource
	for _, r := range resources {
		if r.ID != resourceID {
			filtered = append(filtered, r)
		}
	}

	file, err := os.OpenFile(path, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, r := range filtered {
		line := r.ID + " " + r.URL + "\n"
		if !r.Primary {
			line = "~ " + line
		}
		if _, err = file.WriteString(line); err != nil {
			return err
		}
	}

	return nil
}
