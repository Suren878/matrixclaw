package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	reverseGeocodeOSMToolName = "reverse_geocode_osm"
	nearbyPlacesOSMToolName   = "nearby_places_osm"

	defaultOSMNominatimBaseURL = "https://nominatim.openstreetmap.org"
	defaultOSMOverpassEndpoint = "https://overpass-api.de/api/interpreter"
	defaultOSMUserAgent        = "matrixclaw/self-hosted"

	defaultOSMNearbyRadiusMeters = 1500
	maxOSMNearbyRadiusMeters     = 5000
	defaultOSMNearbyLimit        = 10
	maxOSMNearbyLimit            = 30

	osmHTTPTimeout       = 15 * time.Second
	osmReverseCacheTTL   = 24 * time.Hour
	osmNearbyCacheTTL    = 15 * time.Minute
	osmNominatimInterval = time.Second
)

var (
	reverseGeocodeOSMInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "latitude": {"type": "number", "minimum": -90, "maximum": 90},
    "longitude": {"type": "number", "minimum": -180, "maximum": 180},
    "language": {"type": "string", "description": "Preferred response language, for example ru, en, kk"}
  },
  "required": ["latitude", "longitude"],
  "additionalProperties": false
}`)
	nearbyPlacesOSMInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "latitude": {"type": "number", "minimum": -90, "maximum": 90},
    "longitude": {"type": "number", "minimum": -180, "maximum": 180},
    "radius_m": {"type": "integer", "minimum": 100, "maximum": 5000, "description": "Search radius in meters (default 1500)"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 30, "description": "Maximum places to return (default 10)"},
    "categories": {
      "type": "array",
      "items": {"type": "string", "enum": ["restaurant", "cafe", "fast_food", "food_court", "bar", "pub"]},
      "description": "OSM amenity categories to search; defaults to food and drink places"
    }
  },
  "required": ["latitude", "longitude"],
  "additionalProperties": false
}`)
)

type OSMConfig struct {
	NominatimBaseURL string
	OverpassEndpoint string
	UserAgent        string
	HTTPClient       *http.Client
	ReverseCacheTTL  time.Duration
	NearbyCacheTTL   time.Duration
	Disabled         bool
}

type OSMService struct {
	nominatimBaseURL string
	overpassEndpoint string
	userAgent        string
	client           *http.Client
	reverseCacheTTL  time.Duration
	nearbyCacheTTL   time.Duration
	disabled         bool

	cacheMu      sync.Mutex
	reverseCache map[string]cachedOSMReverse
	nearbyCache  map[string]cachedOSMNearby

	nominatimMu   sync.Mutex
	lastNominatim time.Time
	overpassMu    sync.Mutex
}

type cachedOSMReverse struct {
	expires time.Time
	result  OSMReverseGeocodeResult
}

type cachedOSMNearby struct {
	expires time.Time
	result  OSMNearbyPlacesResult
}

type OSMReverseGeocodeParams struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Language  string  `json:"language,omitempty"`
}

type OSMNearbyPlacesParams struct {
	Latitude     float64  `json:"latitude"`
	Longitude    float64  `json:"longitude"`
	RadiusMeters int      `json:"radius_m,omitempty"`
	Limit        int      `json:"limit,omitempty"`
	Categories   []string `json:"categories,omitempty"`
}

type OSMReverseGeocodeResult struct {
	Latitude    float64           `json:"latitude"`
	Longitude   float64           `json:"longitude"`
	DisplayName string            `json:"display_name,omitempty"`
	Address     OSMAddress        `json:"address,omitempty"`
	AddressTags map[string]string `json:"address_tags,omitempty"`
	OSMType     string            `json:"osm_type,omitempty"`
	OSMID       int64             `json:"osm_id,omitempty"`
	Category    string            `json:"category,omitempty"`
	Type        string            `json:"type,omitempty"`
	Source      string            `json:"source"`
	Attribution string            `json:"attribution"`
}

type OSMAddress struct {
	HouseNumber   string `json:"house_number,omitempty"`
	Road          string `json:"road,omitempty"`
	Neighbourhood string `json:"neighbourhood,omitempty"`
	Suburb        string `json:"suburb,omitempty"`
	District      string `json:"district,omitempty"`
	City          string `json:"city,omitempty"`
	County        string `json:"county,omitempty"`
	State         string `json:"state,omitempty"`
	Postcode      string `json:"postcode,omitempty"`
	Country       string `json:"country,omitempty"`
	CountryCode   string `json:"country_code,omitempty"`
}

type OSMNearbyPlacesResult struct {
	Latitude     float64    `json:"latitude"`
	Longitude    float64    `json:"longitude"`
	RadiusMeters int        `json:"radius_m"`
	Categories   []string   `json:"categories"`
	Places       []OSMPlace `json:"places"`
	Source       string     `json:"source"`
	Attribution  string     `json:"attribution"`
}

type OSMPlace struct {
	Name           string            `json:"name"`
	Amenity        string            `json:"amenity,omitempty"`
	Cuisine        string            `json:"cuisine,omitempty"`
	DistanceMeters int               `json:"distance_m"`
	Latitude       float64           `json:"latitude"`
	Longitude      float64           `json:"longitude"`
	Address        string            `json:"address,omitempty"`
	AddressTags    map[string]string `json:"address_tags,omitempty"`
	OpeningHours   string            `json:"opening_hours,omitempty"`
	Phone          string            `json:"phone,omitempty"`
	Website        string            `json:"website,omitempty"`
	OSMType        string            `json:"osm_type"`
	OSMID          int64             `json:"osm_id"`
	OSMURL         string            `json:"osm_url,omitempty"`
}

type reverseGeocodeOSMExecutor struct {
	service *OSMService
}

type nearbyPlacesOSMExecutor struct {
	service *OSMService
}

func NewOSMServiceFromEnv() *OSMService {
	cfg := OSMConfig{
		NominatimBaseURL: os.Getenv("MATRIXCLAW_OSM_NOMINATIM_URL"),
		OverpassEndpoint: os.Getenv("MATRIXCLAW_OSM_OVERPASS_URL"),
		UserAgent:        os.Getenv("MATRIXCLAW_OSM_USER_AGENT"),
		Disabled:         envBoolFalse(os.Getenv("MATRIXCLAW_OSM_ENABLED")),
	}
	return NewOSMService(cfg)
}

func NewOSMService(cfg OSMConfig) *OSMService {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: osmHTTPTimeout}
	}
	if strings.TrimSpace(cfg.NominatimBaseURL) == "" {
		cfg.NominatimBaseURL = defaultOSMNominatimBaseURL
	}
	if strings.TrimSpace(cfg.OverpassEndpoint) == "" {
		cfg.OverpassEndpoint = defaultOSMOverpassEndpoint
	}
	if strings.TrimSpace(cfg.UserAgent) == "" {
		cfg.UserAgent = defaultOSMUserAgent
	}
	if cfg.ReverseCacheTTL <= 0 {
		cfg.ReverseCacheTTL = osmReverseCacheTTL
	}
	if cfg.NearbyCacheTTL <= 0 {
		cfg.NearbyCacheTTL = osmNearbyCacheTTL
	}
	return &OSMService{
		nominatimBaseURL: strings.TrimRight(strings.TrimSpace(cfg.NominatimBaseURL), "/"),
		overpassEndpoint: strings.TrimSpace(cfg.OverpassEndpoint),
		userAgent:        strings.TrimSpace(cfg.UserAgent),
		client:           cfg.HTTPClient,
		reverseCacheTTL:  cfg.ReverseCacheTTL,
		nearbyCacheTTL:   cfg.NearbyCacheTTL,
		disabled:         cfg.Disabled,
		reverseCache:     map[string]cachedOSMReverse{},
		nearbyCache:      map[string]cachedOSMNearby{},
	}
}

func NewOSMGeoExecutors(service *OSMService) []Executor {
	if service == nil {
		service = NewOSMServiceFromEnv()
	}
	return []Executor{
		NewReverseGeocodeOSMExecutor(service),
		NewNearbyPlacesOSMExecutor(service),
	}
}

func NewReverseGeocodeOSMExecutor(service *OSMService) Executor {
	if service == nil {
		service = NewOSMServiceFromEnv()
	}
	return &reverseGeocodeOSMExecutor{service: service}
}

func NewNearbyPlacesOSMExecutor(service *OSMService) Executor {
	if service == nil {
		service = NewOSMServiceFromEnv()
	}
	return &nearbyPlacesOSMExecutor{service: service}
}

func (e *reverseGeocodeOSMExecutor) Spec() Spec {
	return Spec{
		ID:              reverseGeocodeOSMToolName,
		Name:            "ReverseGeocodeOSM",
		Description:     "Resolve exact latitude/longitude coordinates to OpenStreetMap address components using Nominatim. Use this before text-based local search; keep coordinates authoritative.",
		Risk:            RiskSafe,
		Effect:          EffectReadOnly,
		ApprovalMode:    ApprovalNever,
		Namespace:       namespaceCoreWeb,
		Category:        CategoryWeb,
		Profiles:        []Profile{ProfileCoding, ProfileWeb},
		OutputKind:      OutputWebContent,
		InputJSONSchema: reverseGeocodeOSMInputSchema,
	}
}

func (e *reverseGeocodeOSMExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params OSMReverseGeocodeParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(reverseGeocodeOSMToolName, err)
	}
	result, err := e.service.ReverseGeocode(ctx, params)
	if err != nil {
		return Result{Content: fmt.Sprintf("reverse_geocode_osm failed: %v", err), IsError: true}, nil
	}
	return Result{
		Content:  formatOSMReverseGeocode(result),
		Metadata: result,
	}, nil
}

func (e *nearbyPlacesOSMExecutor) Spec() Spec {
	return Spec{
		ID:              nearbyPlacesOSMToolName,
		Name:            "NearbyPlacesOSM",
		Description:     "Find nearby food and drink places by exact coordinates using OpenStreetMap Overpass data. Use latitude/longitude and radius, not a guessed street or district.",
		Risk:            RiskSafe,
		Effect:          EffectReadOnly,
		ApprovalMode:    ApprovalNever,
		Namespace:       namespaceCoreWeb,
		Category:        CategoryWeb,
		Profiles:        []Profile{ProfileCoding, ProfileWeb},
		OutputKind:      OutputSearchResults,
		InputJSONSchema: nearbyPlacesOSMInputSchema,
	}
}

func (e *nearbyPlacesOSMExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params OSMNearbyPlacesParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(nearbyPlacesOSMToolName, err)
	}
	result, err := e.service.NearbyPlaces(ctx, params)
	if err != nil {
		return Result{Content: fmt.Sprintf("nearby_places_osm failed: %v", err), IsError: true}, nil
	}
	return Result{
		Content:  formatOSMNearbyPlaces(result),
		Metadata: result,
	}, nil
}

func (s *OSMService) ReverseGeocode(ctx context.Context, params OSMReverseGeocodeParams) (OSMReverseGeocodeResult, error) {
	if s == nil {
		s = NewOSMServiceFromEnv()
	}
	if s.disabled {
		return OSMReverseGeocodeResult{}, fmt.Errorf("osm geo provider is disabled")
	}
	if err := validateCoordinates(params.Latitude, params.Longitude); err != nil {
		return OSMReverseGeocodeResult{}, err
	}
	params.Language = strings.TrimSpace(params.Language)
	key := fmt.Sprintf("%.5f,%.5f,%s", params.Latitude, params.Longitude, strings.ToLower(params.Language))
	if result, ok := s.reverseCacheGet(key); ok {
		return result, nil
	}
	if err := s.waitNominatimSlot(ctx); err != nil {
		return OSMReverseGeocodeResult{}, err
	}

	reqURL, err := url.Parse(s.nominatimBaseURL + "/reverse")
	if err != nil {
		return OSMReverseGeocodeResult{}, err
	}
	q := reqURL.Query()
	q.Set("format", "jsonv2")
	q.Set("addressdetails", "1")
	q.Set("zoom", "18")
	q.Set("lat", strconv.FormatFloat(params.Latitude, 'f', 6, 64))
	q.Set("lon", strconv.FormatFloat(params.Longitude, 'f', 6, 64))
	if params.Language != "" {
		q.Set("accept-language", params.Language)
	}
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return OSMReverseGeocodeResult{}, err
	}
	s.setHeaders(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return OSMReverseGeocodeResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return OSMReverseGeocodeResult{}, fmt.Errorf("nominatim returned %d", resp.StatusCode)
	}

	var out struct {
		Lat         string         `json:"lat"`
		Lon         string         `json:"lon"`
		DisplayName string         `json:"display_name"`
		Address     map[string]any `json:"address"`
		OSMType     string         `json:"osm_type"`
		OSMID       int64          `json:"osm_id"`
		Category    string         `json:"category"`
		Type        string         `json:"type"`
		Licence     string         `json:"licence"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return OSMReverseGeocodeResult{}, err
	}
	addressTags := normalizeStringMap(out.Address)
	lat := params.Latitude
	if parsed, err := strconv.ParseFloat(out.Lat, 64); err == nil {
		lat = parsed
	}
	lon := params.Longitude
	if parsed, err := strconv.ParseFloat(out.Lon, 64); err == nil {
		lon = parsed
	}
	result := OSMReverseGeocodeResult{
		Latitude:    lat,
		Longitude:   lon,
		DisplayName: strings.TrimSpace(out.DisplayName),
		Address:     osmAddressFromTags(addressTags),
		AddressTags: addressTags,
		OSMType:     strings.TrimSpace(out.OSMType),
		OSMID:       out.OSMID,
		Category:    strings.TrimSpace(out.Category),
		Type:        strings.TrimSpace(out.Type),
		Source:      "nominatim.openstreetmap.org",
		Attribution: firstNonEmpty(strings.TrimSpace(out.Licence), "OpenStreetMap contributors, ODbL"),
	}
	s.reverseCacheSet(key, result)
	return result, nil
}

func (s *OSMService) NearbyPlaces(ctx context.Context, params OSMNearbyPlacesParams) (OSMNearbyPlacesResult, error) {
	if s == nil {
		s = NewOSMServiceFromEnv()
	}
	if s.disabled {
		return OSMNearbyPlacesResult{}, fmt.Errorf("osm geo provider is disabled")
	}
	if err := validateCoordinates(params.Latitude, params.Longitude); err != nil {
		return OSMNearbyPlacesResult{}, err
	}
	if params.RadiusMeters <= 0 {
		params.RadiusMeters = defaultOSMNearbyRadiusMeters
	}
	if params.RadiusMeters > maxOSMNearbyRadiusMeters {
		params.RadiusMeters = maxOSMNearbyRadiusMeters
	}
	if params.Limit <= 0 {
		params.Limit = defaultOSMNearbyLimit
	}
	if params.Limit > maxOSMNearbyLimit {
		params.Limit = maxOSMNearbyLimit
	}
	categories := normalizeOSMCategories(params.Categories)
	key := fmt.Sprintf("%.5f,%.5f,%d,%d,%s", params.Latitude, params.Longitude, params.RadiusMeters, params.Limit, strings.Join(categories, ","))
	if result, ok := s.nearbyCacheGet(key); ok {
		return result, nil
	}

	query := buildOverpassNearbyQuery(params.Latitude, params.Longitude, params.RadiusMeters, params.Limit, categories)
	form := url.Values{}
	form.Set("data", query)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.overpassEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return OSMNearbyPlacesResult{}, err
	}
	s.setHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	s.overpassMu.Lock()
	resp, err := s.client.Do(req)
	s.overpassMu.Unlock()
	if err != nil {
		return OSMNearbyPlacesResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return OSMNearbyPlacesResult{}, fmt.Errorf("overpass returned %d", resp.StatusCode)
	}

	var out struct {
		Elements []struct {
			Type   string            `json:"type"`
			ID     int64             `json:"id"`
			Lat    *float64          `json:"lat"`
			Lon    *float64          `json:"lon"`
			Center *overpassCenter   `json:"center"`
			Tags   map[string]string `json:"tags"`
		} `json:"elements"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return OSMNearbyPlacesResult{}, err
	}

	places := make([]OSMPlace, 0, len(out.Elements))
	for _, element := range out.Elements {
		place, ok := osmPlaceFromElement(params.Latitude, params.Longitude, params.RadiusMeters, element.Type, element.ID, element.Lat, element.Lon, element.Center, element.Tags)
		if ok {
			places = append(places, place)
		}
	}
	sort.SliceStable(places, func(i, j int) bool {
		return places[i].DistanceMeters < places[j].DistanceMeters
	})
	if len(places) > params.Limit {
		places = places[:params.Limit]
	}
	result := OSMNearbyPlacesResult{
		Latitude:     params.Latitude,
		Longitude:    params.Longitude,
		RadiusMeters: params.RadiusMeters,
		Categories:   categories,
		Places:       places,
		Source:       "overpass-api.de",
		Attribution:  "OpenStreetMap contributors, ODbL",
	}
	s.nearbyCacheSet(key, result)
	return result, nil
}

type overpassCenter struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

func validateCoordinates(lat, lon float64) error {
	if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) {
		return fmt.Errorf("coordinates must be finite numbers")
	}
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return fmt.Errorf("coordinates out of range")
	}
	return nil
}

func (s *OSMService) waitNominatimSlot(ctx context.Context) error {
	s.nominatimMu.Lock()
	defer s.nominatimMu.Unlock()
	if !s.lastNominatim.IsZero() {
		wait := time.Until(s.lastNominatim.Add(osmNominatimInterval))
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	s.lastNominatim = time.Now()
	return nil
}

func (s *OSMService) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", s.userAgent)
}

func (s *OSMService) reverseCacheGet(key string) (OSMReverseGeocodeResult, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry, ok := s.reverseCache[key]
	if !ok || time.Now().After(entry.expires) {
		if ok {
			delete(s.reverseCache, key)
		}
		return OSMReverseGeocodeResult{}, false
	}
	return entry.result, true
}

func (s *OSMService) reverseCacheSet(key string, result OSMReverseGeocodeResult) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.reverseCache[key] = cachedOSMReverse{expires: time.Now().Add(s.reverseCacheTTL), result: result}
}

func (s *OSMService) nearbyCacheGet(key string) (OSMNearbyPlacesResult, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry, ok := s.nearbyCache[key]
	if !ok || time.Now().After(entry.expires) {
		if ok {
			delete(s.nearbyCache, key)
		}
		return OSMNearbyPlacesResult{}, false
	}
	return entry.result, true
}

func (s *OSMService) nearbyCacheSet(key string, result OSMNearbyPlacesResult) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.nearbyCache[key] = cachedOSMNearby{expires: time.Now().Add(s.nearbyCacheTTL), result: result}
}

func normalizeOSMCategories(categories []string) []string {
	allowed := map[string]struct{}{
		"restaurant": {},
		"cafe":       {},
		"fast_food":  {},
		"food_court": {},
		"bar":        {},
		"pub":        {},
	}
	if len(categories) == 0 {
		return []string{"restaurant", "cafe", "fast_food", "food_court", "bar", "pub"}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(categories))
	for _, category := range categories {
		category = strings.ToLower(strings.TrimSpace(category))
		if _, ok := allowed[category]; !ok {
			continue
		}
		if _, ok := seen[category]; ok {
			continue
		}
		seen[category] = struct{}{}
		out = append(out, category)
	}
	if len(out) == 0 {
		return []string{"restaurant", "cafe", "fast_food", "food_court", "bar", "pub"}
	}
	return out
}

func buildOverpassNearbyQuery(lat, lon float64, radiusMeters, limit int, categories []string) string {
	amenityRegex := "^(" + strings.Join(categories, "|") + ")$"
	return fmt.Sprintf(`[out:json][timeout:10];
(
  node(around:%d,%.6f,%.6f)[amenity~%q];
  way(around:%d,%.6f,%.6f)[amenity~%q];
  relation(around:%d,%.6f,%.6f)[amenity~%q];
);
out center tags %d;`,
		radiusMeters, lat, lon, amenityRegex,
		radiusMeters, lat, lon, amenityRegex,
		radiusMeters, lat, lon, amenityRegex,
		limit*3,
	)
}

func osmPlaceFromElement(originLat, originLon float64, radiusMeters int, typ string, id int64, lat, lon *float64, center *overpassCenter, tags map[string]string) (OSMPlace, bool) {
	if len(tags) == 0 {
		return OSMPlace{}, false
	}
	amenity := strings.TrimSpace(tags["amenity"])
	if amenity == "" {
		return OSMPlace{}, false
	}
	var placeLat, placeLon float64
	switch {
	case lat != nil && lon != nil:
		placeLat, placeLon = *lat, *lon
	case center != nil:
		placeLat, placeLon = center.Lat, center.Lon
	default:
		return OSMPlace{}, false
	}
	name := firstNonEmpty(tags["name"], tags["name:ru"], tags["name:en"], tags["brand"])
	name = strings.TrimSpace(name)
	if name == "" {
		return OSMPlace{}, false
	}
	distance := int(math.Round(haversineMeters(originLat, originLon, placeLat, placeLon)))
	if distance > radiusMeters+5 {
		return OSMPlace{}, false
	}
	addressTags := osmAddressTags(tags)
	return OSMPlace{
		Name:           name,
		Amenity:        amenity,
		Cuisine:        strings.TrimSpace(tags["cuisine"]),
		DistanceMeters: distance,
		Latitude:       placeLat,
		Longitude:      placeLon,
		Address:        formatOSMAddressTags(addressTags),
		AddressTags:    addressTags,
		OpeningHours:   strings.TrimSpace(tags["opening_hours"]),
		Phone:          firstNonEmpty(tags["phone"], tags["contact:phone"]),
		Website:        firstNonEmpty(tags["website"], tags["contact:website"]),
		OSMType:        strings.TrimSpace(typ),
		OSMID:          id,
		OSMURL:         osmElementURL(typ, id),
	}, true
}

func osmAddressTags(tags map[string]string) map[string]string {
	out := map[string]string{}
	for _, key := range []string{"addr:housenumber", "addr:street", "addr:city", "addr:postcode", "addr:country"} {
		if value := strings.TrimSpace(tags[key]); value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeStringMap(values map[string]any) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range values {
		if key = strings.TrimSpace(key); key == "" {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				out[key] = strings.TrimSpace(typed)
			}
		case float64:
			out[key] = strconv.FormatFloat(typed, 'f', -1, 64)
		case bool:
			out[key] = strconv.FormatBool(typed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func osmAddressFromTags(tags map[string]string) OSMAddress {
	return OSMAddress{
		HouseNumber:   strings.TrimSpace(tags["house_number"]),
		Road:          strings.TrimSpace(tags["road"]),
		Neighbourhood: firstNonEmpty(tags["neighbourhood"], tags["quarter"]),
		Suburb:        strings.TrimSpace(tags["suburb"]),
		District:      firstNonEmpty(tags["city_district"], tags["district"], tags["borough"]),
		City:          firstNonEmpty(tags["city"], tags["town"], tags["village"], tags["municipality"]),
		County:        strings.TrimSpace(tags["county"]),
		State:         strings.TrimSpace(tags["state"]),
		Postcode:      strings.TrimSpace(tags["postcode"]),
		Country:       strings.TrimSpace(tags["country"]),
		CountryCode:   strings.ToUpper(strings.TrimSpace(tags["country_code"])),
	}
}

func formatOSMReverseGeocode(result OSMReverseGeocodeResult) string {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "reverse_geocode_osm: %.6f, %.6f\n", result.Latitude, result.Longitude)
	if result.DisplayName != "" {
		_, _ = fmt.Fprintf(&b, "display_name: %s\n", result.DisplayName)
	}
	if address := formatOSMAddress(result.Address); address != "" {
		_, _ = fmt.Fprintf(&b, "address: %s\n", address)
	}
	if result.Address.CountryCode != "" {
		_, _ = fmt.Fprintf(&b, "country_code: %s\n", result.Address.CountryCode)
	}
	if result.OSMType != "" && result.OSMID != 0 {
		_, _ = fmt.Fprintf(&b, "osm: %s/%d\n", result.OSMType, result.OSMID)
	}
	_, _ = fmt.Fprintf(&b, "source: %s\nattribution: %s", result.Source, result.Attribution)
	return strings.TrimSpace(b.String())
}

func formatOSMNearbyPlaces(result OSMNearbyPlacesResult) string {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "nearby_places_osm: %d places within %dm of %.6f, %.6f\n", len(result.Places), result.RadiusMeters, result.Latitude, result.Longitude)
	_, _ = fmt.Fprintf(&b, "categories: %s\n", strings.Join(result.Categories, ", "))
	if len(result.Places) == 0 {
		_, _ = fmt.Fprintf(&b, "No matching OpenStreetMap places were found. Do not invent nearby places from unrelated text search results.\n")
	}
	for i, place := range result.Places {
		_, _ = fmt.Fprintf(&b, "\n%d. %s\n", i+1, place.Name)
		_, _ = fmt.Fprintf(&b, "   distance: %dm\n", place.DistanceMeters)
		if place.Amenity != "" {
			_, _ = fmt.Fprintf(&b, "   amenity: %s\n", place.Amenity)
		}
		if place.Cuisine != "" {
			_, _ = fmt.Fprintf(&b, "   cuisine: %s\n", place.Cuisine)
		}
		if place.Address != "" {
			_, _ = fmt.Fprintf(&b, "   address: %s\n", place.Address)
		}
		if place.OpeningHours != "" {
			_, _ = fmt.Fprintf(&b, "   opening_hours: %s\n", place.OpeningHours)
		}
		if place.Phone != "" {
			_, _ = fmt.Fprintf(&b, "   phone: %s\n", place.Phone)
		}
		if place.Website != "" {
			_, _ = fmt.Fprintf(&b, "   website: %s\n", place.Website)
		}
		if place.OSMURL != "" {
			_, _ = fmt.Fprintf(&b, "   osm: %s\n", place.OSMURL)
		}
	}
	_, _ = fmt.Fprintf(&b, "\nsource: %s\nattribution: %s", result.Source, result.Attribution)
	return strings.TrimSpace(b.String())
}

func formatOSMAddress(address OSMAddress) string {
	parts := []string{}
	if street := formatStreetAddress(address.Road, address.HouseNumber); street != "" {
		parts = append(parts, street)
	}
	for _, value := range []string{address.Neighbourhood, address.Suburb, address.District, address.City, address.County, address.State, address.Postcode, address.Country} {
		if value = strings.TrimSpace(value); value != "" && !containsString(parts, value) {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, ", ")
}

func formatOSMAddressTags(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	parts := []string{}
	if street := formatStreetAddress(tags["addr:street"], tags["addr:housenumber"]); street != "" {
		parts = append(parts, street)
	}
	for _, key := range []string{"addr:city", "addr:postcode", "addr:country"} {
		if value := strings.TrimSpace(tags[key]); value != "" && !containsString(parts, value) {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, ", ")
}

func formatStreetAddress(road, houseNumber string) string {
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

func osmElementURL(typ string, id int64) string {
	typ = strings.TrimSpace(typ)
	if typ == "" || id == 0 {
		return ""
	}
	return fmt.Sprintf("https://www.openstreetmap.org/%s/%d", typ, id)
}

func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMeters = 6371000.0
	toRadians := func(v float64) float64 { return v * math.Pi / 180 }
	dLat := toRadians(lat2 - lat1)
	dLon := toRadians(lon2 - lon1)
	rLat1 := toRadians(lat1)
	rLat2 := toRadians(lat2)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(rLat1)*math.Cos(rLat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusMeters * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func envBoolFalse(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "no", "off", "disabled":
		return true
	default:
		return false
	}
}
