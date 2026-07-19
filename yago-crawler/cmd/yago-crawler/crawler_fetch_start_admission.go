package main

import (
	"context"
	"fmt"
)

type crawlerFetchStartAdmission interface {
	Wait(context.Context) error
}

type orderedCrawlerFetchStartAdmission struct {
	process crawlerFetchStartAdmission
	fleet   crawlerFetchStartAdmission
}

func newOrderedCrawlerFetchStartAdmission(
	process crawlerFetchStartAdmission,
	fleet crawlerFetchStartAdmission,
) orderedCrawlerFetchStartAdmission {
	return orderedCrawlerFetchStartAdmission{process: process, fleet: fleet}
}

func (admission orderedCrawlerFetchStartAdmission) Wait(ctx context.Context) error {
	if err := admission.process.Wait(ctx); err != nil {
		return fmt.Errorf("wait for process fetch-start admission: %w", err)
	}

	if err := admission.fleet.Wait(ctx); err != nil {
		return fmt.Errorf("wait for fleet fetch-start admission: %w", err)
	}

	return nil
}
