package cost

// On-demand hourly USD, us-east-1 / equivalent region baseline.
// Unknown types fall back to per-resource Rates. Regional variance is
// why node costs are labeled confidence=estimate by callers.
var instanceHourlyUSD = map[string]float64{
	// AWS
	"t3.medium":   0.0416,
	"t3.large":    0.0832,
	"m5.large":    0.096,
	"m5.xlarge":   0.192,
	"m5.2xlarge":  0.384,
	"c5.xlarge":   0.17,
	"c5.2xlarge":  0.34,
	"r5.xlarge":   0.252,
	"p3.2xlarge":  3.06,
	"g4dn.xlarge": 0.526,
	// GCP
	"e2-standard-2": 0.067,
	"e2-standard-4": 0.134,
	"n2-standard-4": 0.194,
	"n2-standard-8": 0.388,
	// Azure
	"Standard_D2s_v3": 0.096,
	"Standard_D4s_v3": 0.192,
}
