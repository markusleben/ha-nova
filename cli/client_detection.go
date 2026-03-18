package main

func detectInstalledClients(paths runtimePaths) ([]string, error) {
	clients := []string{}
	entries, err := loadClientRegistry(paths)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		status := evaluateClientStatus(paths, installState{}, entry)
		if status.Attached && status.RuntimeDetected {
			clients = append(clients, entry.ID)
		}
	}
	return normalizeClients(clients), nil
}
