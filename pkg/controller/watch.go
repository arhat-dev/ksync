package controller

import (
	"arhat.dev/pkg/hashhelper"
	"arhat.dev/pkg/log"
	"arhat.dev/pkg/queue"
)

func (c *Controller) updateTriggerSourceHashes(tsh map[configRef]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for t, h := range tsh {
		tr := t
		c.reloadTriggerSourceHash[tr] = h
	}
}

func (c *Controller) removeTriggerSourceHashes(tsh map[configRef]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for t := range tsh {
		delete(c.reloadTriggerSourceHash, t)
	}
}

func buildTriggerSourceHash(
	kind configKind,
	namespace, name string,
	stringData map[string]string,
	binaryData map[string][]byte,
) map[configRef]string {
	data := make(map[string][]byte)
	for k := range binaryData {
		data[k] = binaryData[k]
	}

	for k := range stringData {
		data[k] = []byte(stringData[k])
	}

	var allData []byte
	result := make(map[configRef]string)
	for k, v := range data {
		result[createConfigRef(kind, namespace, name, k)] = hashhelper.Sha256SumHex(v)

		allData = append(allData, v...)
	}

	result[createConfigRef(kind, namespace, name, "")] = hashhelper.Sha256SumHex(allData)

	return result
}

func (c *Controller) notifyUpdate(baseLogger log.Interface, triggerSourceHashUpdate map[configRef]string) {
	canBeReloadedBy := make(map[configRef]struct{})

	func() {
		c.mu.Lock()
		defer c.mu.Unlock()

		for t, h := range triggerSourceHashUpdate {
			tr := t
			oldHash, ok := c.reloadTriggerSourceHash[tr]
			if ok && oldHash != h {
				// must have old hash and old hash must be different to trigger reload
				canBeReloadedBy[tr] = struct{}{}
			}

			c.reloadTriggerSourceHash[tr] = h
		}
	}()

	if len(canBeReloadedBy) == 0 {
		baseLogger.V("no reload trigger will be fired")
		return
	}

	if baseLogger.Enabled(log.LevelVerbose) {
		var triggerList []string
		for t := range canBeReloadedBy {
			triggerList = append(triggerList, t.String())
		}

		baseLogger.V("will fire reload trigger(s)", log.Strings("triggers", triggerList))
	} else {
		baseLogger.D("will fire reload trigger(s)")
	}

	go func() {
		c.mu.RLock()
		defer c.mu.RUnlock()

		toBeReloaded := make(map[reloadObjectKey]struct{})

		for t := range canBeReloadedBy {
			for r := range c.reloadTriggerIndex[t] {
				baseLogger.V("found reload target", log.String("target", r.String()))
				toBeReloaded[r] = struct{}{}
			}
		}

		for r := range toBeReloaded {
			logger := baseLogger.WithFields(log.String("target", r.String()))
			logger.V("scheduling reloading")

			c.reloadRec.Update(r, nil, &reloadSpec{reloadObjectKey: r, triggers: canBeReloadedBy})
			err := c.reloadRec.Schedule(queue.Job{Action: queue.ActionAdd, Key: r}, c.reloadDelay)
			if err != nil {
				logger.E("failed to schedule reload", log.Error(err))
				continue
			}

			logger.V("reload scheduled")
		}
	}()

	go func() {
		c.syncerMu.RLock()
		defer c.syncerMu.RUnlock()

		for t := range canBeReloadedBy {
			spec, ok := c.syncerTriggerIndex[t]
			if !ok {
				continue
			}

			logger := baseLogger.WithFields(log.String("target", t.String()))
			logger.V("scheduling syncer config update")

			c.syncRec.Update(t, nil, spec)
			err := c.syncRec.Schedule(queue.Job{Action: queue.ActionAdd, Key: t}, 0)
			if err != nil {
				logger.E("failed to schedule syncer config update", log.Error(err))
				continue
			}

			logger.V("syncer config update scheduled")
		}
	}()
}
