package crawlresults

import "context"

func (c *IngestConsumer) admitStorageGrowth(
	ctx context.Context,
	deliveries []IngestDelivery,
) bool {
	if len(deliveries) == 0 || c.growthAdmission == nil {
		return true
	}
	if err := c.growthAdmission.CheckGrowth(); err != nil {
		c.redeliverGroup(ctx, deliveries, "storage pressure", nil)

		return false
	}

	return true
}
