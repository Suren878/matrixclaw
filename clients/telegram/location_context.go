package telegram

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/tools"
)

const telegramLocationContextTTL = 30 * time.Minute

const telegramLocationRequestText = "Attach your Telegram location to use nearby context. Open Telegram attachments and send Location. If more than 30 minutes pass, I will ask you to refresh it."

func textNeedsTelegramLocation(text string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(text), " "))
	if normalized == "" {
		return false
	}
	triggers := []string{
		"nearby",
		"near me",
		"around me",
		"close to me",
		"рядом",
		"поблизости",
		"около меня",
		"возле меня",
		"неподалеку",
		"неподалёку",
	}
	for _, trigger := range triggers {
		if strings.Contains(normalized, trigger) {
			return true
		}
	}
	return false
}

func (w *Worker) requestTelegramLocation(ctx context.Context, target chatTarget, text string) error {
	if !target.isChat() {
		return w.sendUserMessage(ctx, target, text)
	}
	_, err := w.sendTelegramMessage(ctx, SendMessageRequest{
		ChatID:      target.chatID,
		Text:        telegramLocationRequestText,
		ReplyMarkup: telegramReplyKeyboardRemove(),
	})
	if err == nil {
		w.rememberPendingLocationRequest(target, text)
	}
	return err
}

func (w *Worker) nowUTC() time.Time {
	if w != nil && w.now != nil {
		return w.now().UTC()
	}
	return time.Now().UTC()
}

func (w *Worker) rememberTelegramLocation(target chatTarget, location Location) {
	if strings.TrimSpace(target.externalKey) == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.locations == nil {
		w.locations = map[string]telegramLocationContext{}
	}
	w.locations[target.externalKey] = telegramLocationContext{
		Location: location,
		SharedAt: w.nowUTC(),
	}
}

func (w *Worker) freshTelegramLocation(target chatTarget) (Location, bool) {
	if strings.TrimSpace(target.externalKey) == "" {
		return Location{}, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	location, ok := w.locations[target.externalKey]
	if !ok {
		return Location{}, false
	}
	if w.nowUTC().Sub(location.SharedAt) > telegramLocationContextTTL {
		return Location{}, false
	}
	return location.Location, true
}

func (w *Worker) rememberPendingLocationRequest(target chatTarget, text string) {
	if strings.TrimSpace(target.externalKey) == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pendingLocations == nil {
		w.pendingLocations = map[string]pendingLocationRequest{}
	}
	w.pendingLocations[target.externalKey] = pendingLocationRequest{
		Text: text,
	}
}

func (w *Worker) takePendingLocationRequest(target chatTarget) (pendingLocationRequest, bool) {
	if strings.TrimSpace(target.externalKey) == "" {
		return pendingLocationRequest{}, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	request, ok := w.pendingLocations[target.externalKey]
	if ok {
		delete(w.pendingLocations, target.externalKey)
	}
	return request, ok
}

func telegramTextWithLocation(text string, location Location) string {
	text = strings.TrimSpace(text)
	locationText := telegramLocationPrompt(location)
	if text == "" {
		return locationText
	}
	return text + "\n\n" + locationText
}

func (w *Worker) telegramTextWithLocationContext(ctx context.Context, text string, location Location, includeNearby bool) string {
	text = strings.TrimSpace(text)
	locationText := telegramLocationPrompt(location)
	if osmContext := w.telegramOSMContext(ctx, location, includeNearby); osmContext != "" {
		locationText += "\n\n" + osmContext
	}
	if text == "" {
		return locationText
	}
	return text + "\n\n" + locationText
}

func (w *Worker) telegramOSMContext(ctx context.Context, location Location, includeNearby bool) string {
	if w == nil || w.geo == nil {
		return ""
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	sections := []string{}
	reverse, err := w.geo.ReverseGeocode(lookupCtx, tools.OSMReverseGeocodeParams{
		Latitude:  location.Latitude,
		Longitude: location.Longitude,
		Language:  "ru,en",
	})
	if err != nil {
		log.Printf("telegram: osm reverse geocode failed lat=%.6f lon=%.6f: %v", location.Latitude, location.Longitude, err)
		sections = append(sections, "OSM reverse-geocoded address: unavailable. Keep the latitude/longitude authoritative.")
	} else {
		sections = append(sections, formatTelegramOSMReverseContext(reverse))
	}

	if includeNearby {
		nearby, err := w.geo.NearbyPlaces(lookupCtx, tools.OSMNearbyPlacesParams{
			Latitude:     location.Latitude,
			Longitude:    location.Longitude,
			RadiusMeters: telegramNearbyRadiusMeters(location),
			Limit:        10,
		})
		if err != nil {
			log.Printf("telegram: osm nearby places failed lat=%.6f lon=%.6f: %v", location.Latitude, location.Longitude, err)
			sections = append(sections, "OSM nearby places: unavailable. Do not guess nearby restaurants from unrelated text search results.")
		} else {
			sections = append(sections, formatTelegramOSMNearbyContext(nearby))
		}
	}

	return strings.Join(sections, "\n\n")
}

func telegramNearbyRadiusMeters(location Location) int {
	radius := 1500
	if location.HorizontalAccuracy > 0 {
		radius = max(radius, int(math.Ceil(location.HorizontalAccuracy*2)))
	}
	return min(radius, 5000)
}

func formatTelegramOSMReverseContext(result tools.OSMReverseGeocodeResult) string {
	lines := []string{"OSM reverse-geocoded address for the exact coordinates:"}
	if result.DisplayName != "" {
		lines = append(lines, "Display name: "+result.DisplayName)
	}
	if address := formatTelegramOSMAddress(result.Address); address != "" {
		lines = append(lines, "Address: "+address)
	}
	componentText := formatTelegramOSMAddressComponents(result.Address)
	if componentText != "" {
		lines = append(lines, "Address components: "+componentText)
	}
	if result.Address.CountryCode != "" {
		lines = append(lines, "Country code: "+result.Address.CountryCode)
	}
	lines = append(lines, "Use this address as context only; the coordinates remain authoritative for nearby search.")
	lines = append(lines, "Source: OpenStreetMap/Nominatim; attribution: "+result.Attribution)
	return strings.Join(lines, "\n")
}

func formatTelegramOSMNearbyContext(result tools.OSMNearbyPlacesResult) string {
	lines := []string{
		fmt.Sprintf("OSM nearby places from Overpass, sorted by distance within %dm:", result.RadiusMeters),
	}
	if len(result.Places) == 0 {
		lines = append(lines, "No food or drink places were found in OpenStreetMap for this radius. Say reliable nearby results were not found instead of inventing places.")
		lines = append(lines, "Source: OpenStreetMap/Overpass; attribution: "+result.Attribution)
		return strings.Join(lines, "\n")
	}
	for i, place := range result.Places {
		parts := []string{
			fmt.Sprintf("%d. %s", i+1, place.Name),
			fmt.Sprintf("%dm", place.DistanceMeters),
		}
		if place.Amenity != "" {
			parts = append(parts, place.Amenity)
		}
		if place.Cuisine != "" {
			parts = append(parts, "cuisine: "+place.Cuisine)
		}
		if place.Address != "" {
			parts = append(parts, "address: "+place.Address)
		}
		if place.OpeningHours != "" {
			parts = append(parts, "hours: "+place.OpeningHours)
		}
		if place.OSMURL != "" {
			parts = append(parts, place.OSMURL)
		}
		lines = append(lines, strings.Join(parts, " | "))
	}
	lines = append(lines, "Use these coordinate-based results for nearby recommendations. Do not replace them with unrelated text-search results from another district or city.")
	lines = append(lines, "Source: OpenStreetMap/Overpass; attribution: "+result.Attribution)
	return strings.Join(lines, "\n")
}

func formatTelegramOSMAddress(address tools.OSMAddress) string {
	parts := []string{}
	if street := formatTelegramStreetAddress(address.Road, address.HouseNumber); street != "" {
		parts = append(parts, street)
	}
	for _, value := range []string{address.Neighbourhood, address.Suburb, address.District, address.City, address.County, address.State, address.Postcode, address.Country} {
		if value = strings.TrimSpace(value); value != "" && !telegramContainsString(parts, value) {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, ", ")
}

func formatTelegramOSMAddressComponents(address tools.OSMAddress) string {
	components := []string{}
	appendComponent := func(key, value string) {
		if value = strings.TrimSpace(value); value != "" {
			components = append(components, key+"="+value)
		}
	}
	appendComponent("house_number", address.HouseNumber)
	appendComponent("road", address.Road)
	appendComponent("neighbourhood", address.Neighbourhood)
	appendComponent("suburb", address.Suburb)
	appendComponent("district", address.District)
	appendComponent("city", address.City)
	appendComponent("county", address.County)
	appendComponent("state", address.State)
	appendComponent("postcode", address.Postcode)
	appendComponent("country", address.Country)
	appendComponent("country_code", address.CountryCode)
	return strings.Join(components, "; ")
}

func formatTelegramStreetAddress(road, houseNumber string) string {
	road = strings.TrimSpace(road)
	houseNumber = strings.TrimSpace(houseNumber)
	switch {
	case road != "" && houseNumber != "":
		return road + ", " + houseNumber
	case road != "":
		return road
	default:
		return houseNumber
	}
}

func telegramContainsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}
