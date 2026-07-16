package adminauth

import (
	"context"
	"strings"
)

const (
	loginNodeStatusUnavailable  = "Unavailable"
	maximumLoginNodeStatusRunes = 256
)

type LoginNodeStatus struct {
	NodeName       string
	SwarmAddress   string
	ProcessorModel string
	ProcessorCount string
	MemoryCapacity string
	DataFreeSpace  string
	Version        string
	Uptime         string
}

type LoginNodeStatusSource interface {
	LoginNodeStatus(context.Context) LoginNodeStatus
}

func normalizedLoginNodeStatus(ctx context.Context, source LoginNodeStatusSource) LoginNodeStatus {
	status := LoginNodeStatus{}
	if source != nil {
		status = source.LoginNodeStatus(ctx)
	}
	status.NodeName = normalizeLoginNodeStatusValue(status.NodeName)
	status.SwarmAddress = normalizeLoginNodeStatusValue(status.SwarmAddress)
	status.ProcessorModel = normalizeLoginNodeStatusValue(status.ProcessorModel)
	status.ProcessorCount = normalizeLoginNodeStatusValue(status.ProcessorCount)
	status.MemoryCapacity = normalizeLoginNodeStatusValue(status.MemoryCapacity)
	status.DataFreeSpace = normalizeLoginNodeStatusValue(status.DataFreeSpace)
	status.Version = normalizeLoginNodeStatusValue(status.Version)
	status.Uptime = normalizeLoginNodeStatusValue(status.Uptime)

	return status
}

func normalizeLoginNodeStatusValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return loginNodeStatusUnavailable
	}
	runes := []rune(value)
	if len(runes) > maximumLoginNodeStatusRunes {
		value = string(runes[:maximumLoginNodeStatusRunes])
	}

	return value
}
