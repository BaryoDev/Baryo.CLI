// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ListModels runs `docker model list --json` and returns the available models.
func ListModels() ([]DockerModel, error) {
	cmd := exec.Command("docker", "model", "list", "--json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker model list failed: %w", err)
	}

	var raw []dockerModelRaw
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse model list: %w", err)
	}

	models := make([]DockerModel, 0, len(raw))
	for _, r := range raw {
		if len(r.Tags) == 0 {
			continue
		}
		tag := r.Tags[0]
		// Extract short name: "docker.io/ai/mistral:latest" -> "ai/mistral"
		name := tag
		name = strings.TrimPrefix(name, "docker.io/")
		if idx := strings.LastIndex(name, ":"); idx != -1 {
			name = name[:idx]
		}

		models = append(models, DockerModel{
			Name:   name,
			Tag:    tag,
			Params: r.Config.Parameters,
			Size:   r.Config.Size,
		})
	}

	return models, nil
}
