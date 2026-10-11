package mapillary

import "strings"

// Seed is a place the finder searches around. Cities are used because
// that is where street-level imagery is dense; RadiusKm lets the search
// wander into suburbs and countryside.
type Seed struct {
	Name     string
	Country  string // ISO 3166-1 alpha-2
	Region   string // continent slug
	Lat, Lng float64
	RadiusKm float64
}

// Regions the game offers. "world" means every seed; "indonesia" is a
// country filter, everything else matches Seed.Region.
var Regions = []string{"world", "indonesia", "asia", "europe", "north-america", "south-america", "africa", "oceania"}

// FilterSeeds returns the seeds that belong to region.
func FilterSeeds(seeds []Seed, region string) []Seed {
	region = strings.ToLower(strings.TrimSpace(region))
	if region == "" || region == "world" {
		return seeds
	}
	var out []Seed
	for _, s := range seeds {
		if (region == "indonesia" && s.Country == "ID") || s.Region == region {
			out = append(out, s)
		}
	}
	return out
}

// ValidRegion reports whether r is a region the game knows.
func ValidRegion(r string) bool {
	for _, x := range Regions {
		if x == r {
			return true
		}
	}
	return false
}

// DefaultSeeds is a hand-picked list of places with decent Mapillary
// coverage, spread across every inhabited continent.
var DefaultSeeds = []Seed{
	// Indonesia
	{"Jakarta", "ID", "asia", -6.2088, 106.8456, 25},
	{"Bandung", "ID", "asia", -6.9175, 107.6191, 20},
	{"Surabaya", "ID", "asia", -7.2575, 112.7521, 20},
	{"Yogyakarta", "ID", "asia", -7.7956, 110.3695, 20},
	{"Semarang", "ID", "asia", -6.9667, 110.4167, 15},
	{"Malang", "ID", "asia", -7.9666, 112.6326, 15},
	{"Denpasar", "ID", "asia", -8.6705, 115.2126, 20},
	{"Medan", "ID", "asia", 3.5952, 98.6722, 20},
	{"Makassar", "ID", "asia", -5.1477, 119.4327, 15},
	{"Palembang", "ID", "asia", -2.9761, 104.7754, 15},
	{"Balikpapan", "ID", "asia", -1.2379, 116.8529, 15},
	{"Manado", "ID", "asia", 1.4748, 124.8421, 12},
	{"Padang", "ID", "asia", -0.9471, 100.4172, 12},
	{"Pontianak", "ID", "asia", -0.0263, 109.3425, 12},
	{"Mataram", "ID", "asia", -8.5833, 116.1167, 12},
	{"Bogor", "ID", "asia", -6.5971, 106.8060, 12},
	{"Solo", "ID", "asia", -7.5755, 110.8243, 12},

	// Rest of Asia
	{"Tokyo", "JP", "asia", 35.6762, 139.6503, 30},
	{"Osaka", "JP", "asia", 34.6937, 135.5023, 20},
	{"Sapporo", "JP", "asia", 43.0618, 141.3545, 15},
	{"Seoul", "KR", "asia", 37.5665, 126.9780, 25},
	{"Busan", "KR", "asia", 35.1796, 129.0756, 15},
	{"Taipei", "TW", "asia", 25.0330, 121.5654, 20},
	{"Hong Kong", "HK", "asia", 22.3193, 114.1694, 12},
	{"Bangkok", "TH", "asia", 13.7563, 100.5018, 25},
	{"Chiang Mai", "TH", "asia", 18.7883, 98.9853, 15},
	{"Kuala Lumpur", "MY", "asia", 3.1390, 101.6869, 20},
	{"Penang", "MY", "asia", 5.4141, 100.3288, 12},
	{"Singapore", "SG", "asia", 1.3521, 103.8198, 12},
	{"Manila", "PH", "asia", 14.5995, 120.9842, 20},
	{"Cebu", "PH", "asia", 10.3157, 123.8854, 12},
	{"Ho Chi Minh City", "VN", "asia", 10.8231, 106.6297, 20},
	{"Hanoi", "VN", "asia", 21.0278, 105.8342, 15},
	{"Phnom Penh", "KH", "asia", 11.5564, 104.9282, 12},
	{"Delhi", "IN", "asia", 28.6139, 77.2090, 25},
	{"Mumbai", "IN", "asia", 19.0760, 72.8777, 20},
	{"Bengaluru", "IN", "asia", 12.9716, 77.5946, 20},
	{"Colombo", "LK", "asia", 6.9271, 79.8612, 12},
	{"Kathmandu", "NP", "asia", 27.7172, 85.3240, 10},
	{"Dhaka", "BD", "asia", 23.8103, 90.4125, 12},
	{"Ulaanbaatar", "MN", "asia", 47.8864, 106.9057, 12},
	{"Almaty", "KZ", "asia", 43.2220, 76.8512, 15},
	{"Tbilisi", "GE", "asia", 41.7151, 44.8271, 12},
	{"Istanbul", "TR", "asia", 41.0082, 28.9784, 25},
	{"Dubai", "AE", "asia", 25.2048, 55.2708, 20},
	{"Tel Aviv", "IL", "asia", 32.0853, 34.7818, 12},
	{"Amman", "JO", "asia", 31.9454, 35.9284, 12},

	// Europe
	{"London", "GB", "europe", 51.5074, -0.1278, 30},
	{"Manchester", "GB", "europe", 53.4808, -2.2426, 20},
	{"Edinburgh", "GB", "europe", 55.9533, -3.1883, 15},
	{"Dublin", "IE", "europe", 53.3498, -6.2603, 15},
	{"Paris", "FR", "europe", 48.8566, 2.3522, 25},
	{"Lyon", "FR", "europe", 45.7640, 4.8357, 20},
	{"Marseille", "FR", "europe", 43.2965, 5.3698, 20},
	{"Brussels", "BE", "europe", 50.8503, 4.3517, 15},
	{"Amsterdam", "NL", "europe", 52.3676, 4.9041, 20},
	{"Berlin", "DE", "europe", 52.5200, 13.4050, 25},
	{"Munich", "DE", "europe", 48.1351, 11.5820, 20},
	{"Hamburg", "DE", "europe", 53.5511, 9.9937, 20},
	{"Zurich", "CH", "europe", 47.3769, 8.5417, 15},
	{"Vienna", "AT", "europe", 48.2082, 16.3738, 15},
	{"Prague", "CZ", "europe", 50.0755, 14.4378, 15},
	{"Warsaw", "PL", "europe", 52.2297, 21.0122, 20},
	{"Krakow", "PL", "europe", 50.0647, 19.9450, 15},
	{"Budapest", "HU", "europe", 47.4979, 19.0402, 15},
	{"Bucharest", "RO", "europe", 44.4268, 26.1025, 15},
	{"Sofia", "BG", "europe", 42.6977, 23.3219, 12},
	{"Athens", "GR", "europe", 37.9838, 23.7275, 15},
	{"Rome", "IT", "europe", 41.9028, 12.4964, 20},
	{"Milan", "IT", "europe", 45.4642, 9.1900, 20},
	{"Naples", "IT", "europe", 40.8518, 14.2681, 15},
	{"Madrid", "ES", "europe", 40.4168, -3.7038, 25},
	{"Barcelona", "ES", "europe", 41.3851, 2.1734, 20},
	{"Seville", "ES", "europe", 37.3891, -5.9845, 15},
	{"Lisbon", "PT", "europe", 38.7223, -9.1393, 15},
	{"Porto", "PT", "europe", 41.1579, -8.6291, 12},
	{"Copenhagen", "DK", "europe", 55.6761, 12.5683, 15},
	{"Oslo", "NO", "europe", 59.9139, 10.7522, 15},
	{"Stockholm", "SE", "europe", 59.3293, 18.0686, 20},
	{"Helsinki", "FI", "europe", 60.1699, 24.9384, 15},
	{"Tallinn", "EE", "europe", 59.4370, 24.7536, 12},
	{"Riga", "LV", "europe", 56.9496, 24.1052, 12},
	{"Vilnius", "LT", "europe", 54.6872, 25.2797, 12},
	{"Kyiv", "UA", "europe", 50.4501, 30.5234, 20},
	{"Belgrade", "RS", "europe", 44.7866, 20.4489, 12},
	{"Zagreb", "HR", "europe", 45.8150, 15.9819, 12},
	{"Ljubljana", "SI", "europe", 46.0569, 14.5058, 10},
	{"Reykjavik", "IS", "europe", 64.1466, -21.9426, 15},

	// North America
	{"New York", "US", "north-america", 40.7128, -74.0060, 30},
	{"Boston", "US", "north-america", 42.3601, -71.0589, 20},
	{"Washington", "US", "north-america", 38.9072, -77.0369, 20},
	{"Chicago", "US", "north-america", 41.8781, -87.6298, 25},
	{"Atlanta", "US", "north-america", 33.7490, -84.3880, 25},
	{"Miami", "US", "north-america", 25.7617, -80.1918, 20},
	{"Houston", "US", "north-america", 29.7604, -95.3698, 25},
	{"Denver", "US", "north-america", 39.7392, -104.9903, 25},
	{"Phoenix", "US", "north-america", 33.4484, -112.0740, 25},
	{"Los Angeles", "US", "north-america", 34.0522, -118.2437, 30},
	{"San Francisco", "US", "north-america", 37.7749, -122.4194, 20},
	{"Seattle", "US", "north-america", 47.6062, -122.3321, 20},
	{"Salt Lake City", "US", "north-america", 40.7608, -111.8910, 20},
	{"Minneapolis", "US", "north-america", 44.9778, -93.2650, 20},
	{"Toronto", "CA", "north-america", 43.6532, -79.3832, 25},
	{"Montreal", "CA", "north-america", 45.5017, -73.5673, 20},
	{"Vancouver", "CA", "north-america", 49.2827, -123.1207, 20},
	{"Calgary", "CA", "north-america", 51.0447, -114.0719, 15},
	{"Mexico City", "MX", "north-america", 19.4326, -99.1332, 25},
	{"Guadalajara", "MX", "north-america", 20.6597, -103.3496, 15},
	{"Guatemala City", "GT", "north-america", 14.6349, -90.5069, 12},
	{"San Jose", "CR", "north-america", 9.9281, -84.0907, 12},
	{"Panama City", "PA", "north-america", 8.9824, -79.5199, 12},

	// South America
	{"Bogota", "CO", "south-america", 4.7110, -74.0721, 20},
	{"Medellin", "CO", "south-america", 6.2442, -75.5812, 15},
	{"Quito", "EC", "south-america", -0.1807, -78.4678, 12},
	{"Lima", "PE", "south-america", -12.0464, -77.0428, 20},
	{"La Paz", "BO", "south-america", -16.4897, -68.1193, 12},
	{"Santiago", "CL", "south-america", -33.4489, -70.6693, 20},
	{"Buenos Aires", "AR", "south-america", -34.6037, -58.3816, 25},
	{"Cordoba", "AR", "south-america", -31.4201, -64.1888, 15},
	{"Montevideo", "UY", "south-america", -34.9011, -56.1645, 12},
	{"Sao Paulo", "BR", "south-america", -23.5505, -46.6333, 30},
	{"Rio de Janeiro", "BR", "south-america", -22.9068, -43.1729, 20},
	{"Curitiba", "BR", "south-america", -25.4284, -49.2733, 15},
	{"Belo Horizonte", "BR", "south-america", -19.9167, -43.9345, 15},
	{"Asuncion", "PY", "south-america", -25.2637, -57.5759, 12},

	// Africa
	{"Cairo", "EG", "africa", 30.0444, 31.2357, 20},
	{"Casablanca", "MA", "africa", 33.5731, -7.5898, 15},
	{"Marrakesh", "MA", "africa", 31.6295, -7.9811, 12},
	{"Tunis", "TN", "africa", 36.8065, 10.1815, 12},
	{"Dakar", "SN", "africa", 14.7167, -17.4677, 12},
	{"Accra", "GH", "africa", 5.6037, -0.1870, 15},
	{"Lagos", "NG", "africa", 6.5244, 3.3792, 20},
	{"Nairobi", "KE", "africa", -1.2921, 36.8219, 15},
	{"Kampala", "UG", "africa", 0.3476, 32.5825, 12},
	{"Kigali", "RW", "africa", -1.9441, 30.0619, 10},
	{"Dar es Salaam", "TZ", "africa", -6.7924, 39.2083, 15},
	{"Addis Ababa", "ET", "africa", 9.0300, 38.7400, 12},
	{"Lusaka", "ZM", "africa", -15.3875, 28.3228, 12},
	{"Johannesburg", "ZA", "africa", -26.2041, 28.0473, 25},
	{"Cape Town", "ZA", "africa", -33.9249, 18.4241, 20},
	{"Durban", "ZA", "africa", -29.8587, 31.0218, 15},
	{"Windhoek", "NA", "africa", -22.5609, 17.0658, 10},
	{"Gaborone", "BW", "africa", -24.6282, 25.9231, 10},

	// Oceania
	{"Sydney", "AU", "oceania", -33.8688, 151.2093, 30},
	{"Melbourne", "AU", "oceania", -37.8136, 144.9631, 30},
	{"Brisbane", "AU", "oceania", -27.4698, 153.0251, 20},
	{"Perth", "AU", "oceania", -31.9505, 115.8605, 20},
	{"Adelaide", "AU", "oceania", -34.9285, 138.6007, 15},
	{"Auckland", "NZ", "oceania", -36.8485, 174.7633, 20},
	{"Wellington", "NZ", "oceania", -41.2865, 174.7762, 12},
	{"Christchurch", "NZ", "oceania", -43.5321, 172.6362, 15},
	{"Suva", "FJ", "oceania", -18.1416, 178.4419, 10},
}
