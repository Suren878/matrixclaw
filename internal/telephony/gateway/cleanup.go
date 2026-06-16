package gateway

import (
	"context"
	"log"
	"strings"
	"time"
)

func (s *Server) cleanupSplitARI(ctx context.Context, channelID string, extraChannels []string, bridgeIDs []string) {
	_ = s.ari.stopSilence(ctx, channelID)
	_ = s.ari.hangup(ctx, channelID)
	for _, id := range extraChannels {
		_ = s.ari.hangup(ctx, id)
	}
	for _, id := range bridgeIDs {
		_ = s.ari.destroyBridge(ctx, id)
	}
}

func (s *Server) cleanupStaleARIOnReady(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	if err := s.events.WaitReady(ctx); err != nil {
		if parent.Err() == nil {
			log.Printf("telephony stale ARI cleanup skipped: %v", err)
		}
		return
	}

	cleanupCtx, cleanupCancel := context.WithTimeout(parent, 15*time.Second)
	defer cleanupCancel()
	if err := s.cleanupStaleARI(cleanupCtx); err != nil && cleanupCtx.Err() == nil {
		log.Printf("telephony stale ARI cleanup failed: %v", err)
	}
}

func (s *Server) cleanupStaleARI(ctx context.Context) error {
	if s == nil || s.ari == nil {
		return nil
	}
	channels, err := s.ari.channels(ctx)
	if err != nil {
		return err
	}
	removedChannels := 0
	for _, channel := range channels {
		if !s.ownsARIChannel(channel) {
			continue
		}
		if s.activeARIChannel(channel.ID) {
			continue
		}
		if err := s.ari.hangup(ctx, channel.ID); err != nil {
			log.Printf("telephony stale ARI channel cleanup failed channel=%s: %v", strings.TrimSpace(channel.ID), err)
			continue
		}
		removedChannels++
	}

	bridges, err := s.ari.bridges(ctx)
	if err != nil {
		return err
	}
	removedBridges := 0
	for _, bridge := range bridges {
		if !s.ownsARIBridge(bridge) {
			continue
		}
		if s.activeARIBridge(bridge.ID) {
			continue
		}
		if err := s.ari.destroyBridge(ctx, bridge.ID); err != nil {
			log.Printf("telephony stale ARI bridge cleanup failed bridge=%s: %v", strings.TrimSpace(bridge.ID), err)
			continue
		}
		removedBridges++
	}
	if removedChannels > 0 || removedBridges > 0 {
		log.Printf("telephony stale ARI cleanup removed channels=%d bridges=%d", removedChannels, removedBridges)
	}
	return nil
}

func (s *Server) ownsARIChannel(channel ariChannel) bool {
	app := strings.TrimSpace(s.cfg.ARIApp)
	id := strings.TrimSpace(channel.ID)
	name := strings.TrimSpace(channel.Name)
	appName := strings.TrimSpace(channel.Dialplan.AppName)
	appData := strings.TrimSpace(channel.Dialplan.AppData)
	if app != "" && strings.EqualFold(appName, "Stasis") && (appData == app || strings.HasPrefix(appData, app+",")) {
		return true
	}
	ownedID := strings.HasPrefix(id, "call_") && (strings.HasSuffix(id, "-call") ||
		strings.HasSuffix(id, "-media") ||
		strings.HasSuffix(id, "-capture-media") ||
		strings.HasSuffix(id, "-playback-media") ||
		strings.HasSuffix(id, "-capture-snoop") ||
		strings.HasSuffix(id, "-playback-snoop"))
	ownedExternalMedia := strings.HasPrefix(name, "UnicastRTP/") && app != "" && strings.Contains(appData, app)
	return ownedID || ownedExternalMedia
}

func (s *Server) ownsARIBridge(bridge ariBridge) bool {
	id := strings.TrimSpace(bridge.ID)
	name := strings.TrimSpace(bridge.Name)
	ownedID := strings.HasPrefix(id, "call_") && strings.HasSuffix(id, "-bridge")
	ownedName := strings.HasPrefix(name, "call_") && strings.HasSuffix(name, "-bridge")
	return ownedID || ownedName
}

func (s *Server) activeARIChannel(channelID string) bool {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, call := range s.calls {
		if call == nil {
			continue
		}
		if channelID == strings.TrimSpace(call.ChannelID) ||
			channelID == strings.TrimSpace(call.ExternalChannelID) ||
			channelID == strings.TrimSpace(call.CaptureExternalChannelID) ||
			channelID == strings.TrimSpace(call.PlaybackExternalChannelID) ||
			channelID == strings.TrimSpace(call.CaptureSnoopChannelID) ||
			channelID == strings.TrimSpace(call.PlaybackSnoopChannelID) {
			return true
		}
	}
	return false
}

func (s *Server) activeARIBridge(bridgeID string) bool {
	bridgeID = strings.TrimSpace(bridgeID)
	if bridgeID == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, call := range s.calls {
		if call != nil && (bridgeID == strings.TrimSpace(call.BridgeID) ||
			bridgeID == strings.TrimSpace(call.CaptureBridgeID) ||
			bridgeID == strings.TrimSpace(call.PlaybackBridgeID)) {
			return true
		}
	}
	return false
}
