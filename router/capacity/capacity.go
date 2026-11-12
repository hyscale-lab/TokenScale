package capacity

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
)

type CapacityProfileData struct {
	InputBuckets  []int          `json:"input_buckets"`
	OutputBuckets []int          `json:"output_buckets"`
	CapacityMap   map[string]float64 `json:"capacity_map"`
}

type RequestType struct {
	InputLength  int // ground truth input length
	InputBucket  int
	OutputBucket int
	Valid        bool
}

func (rt *RequestType) String() string {
	return fmt.Sprintf("input%d_output%d", rt.InputBucket, rt.OutputBucket)
}

// global RNG with fixed seed
var (
	rng = rand.New(rand.NewSource(42)) // 42 = any fixed seed you choose
)

// Load profile data from OFFLINE_PROFILE_PATH once
func LoadProfile(capacityProfileFilePath string) (*CapacityProfileData, error) {
	var result CapacityProfileData

	// Check if path is absolute
	if !filepath.IsAbs(capacityProfileFilePath) {
		projectRoot := os.Getenv("PROJECT_ROOT_PATH")
		if projectRoot == "" {
			return &result, fmt.Errorf("relative path provided but PROJECT_ROOT_PATH not set")
		}
		capacityProfileFilePath = filepath.Join(projectRoot, capacityProfileFilePath)
	}

	// Read file
	data, err := os.ReadFile(capacityProfileFilePath)
	if err != nil {
		return &result, fmt.Errorf("read profile: %w", err)
	}

	// Unmarshal JSON
	if err := json.Unmarshal(data, &result); err != nil {
		return &result, fmt.Errorf("unmarshal profile: %w", err)
	}

	return &result, nil
}

// locateBucket returns:
// - target if it exists in buckets
// - smallest value > target if target does not exist
// - largest value if target >= all bucket values
func locateBucket(target int, buckets []int) int {
	if len(buckets) == 0 {
		panic("locateBucket: empty bucket list")
	}

	// assume buckets is sorted ascending
	for _, b := range buckets {
		if b == target {
			return target
		} else if b > target {
			return b
		}
	}

	// target >= all bucket values
	return buckets[len(buckets)-1]
}

// return a random value in buckets, the value cannot be the correct one
func locateRandomErrorBucket(correct int, buckets []int) int {
	if len(buckets) <= 1 {
		panic("locateRandomErrorBucket: need at least 2 buckets to pick an error")
	}

	// collect all candidates except the correct one
	candidates := make([]int, 0, len(buckets)-1)
	for _, b := range buckets {
		if b != correct {
			candidates = append(candidates, b)
		}
	}

	// pick a random index from candidates
	idx := rng.Intn(len(candidates))
	return candidates[idx]
}

func (profile *CapacityProfileData) PredictRequestType(inputLength int, gtOutputLength int) (RequestType, error) {
	var requestType RequestType
	valid := true
	inputBucket := locateBucket(inputLength, profile.InputBuckets)
	outputBucket := locateBucket(gtOutputLength, profile.OutputBuckets)

	s := os.Getenv("OUTPUT_PREDICTOR_ACCURACY")
	if s != "" && s != "1" {
		accuracy, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return requestType, err
		}
		if accuracy < 1 {
			randomVal := rng.Float64() // This returns a random float between 0 and 1
			if randomVal > accuracy { // Error happens, pick a random output bucket
				valid = false
				outputBucket = locateRandomErrorBucket(outputBucket, profile.OutputBuckets)
			}
		}
	}
	requestType = RequestType{
		InputLength:  inputLength,
		InputBucket:  inputBucket,
		OutputBucket: outputBucket,
		Valid:        valid,
	}
	return requestType, nil
}

func (profile *CapacityProfileData) ObtainCapacity(key string) (float64, error) {
	// Lookup capacity
	capVal, ok := profile.CapacityMap[key]
	if !ok {
		return 0, fmt.Errorf("capacity not found for key: %s", key)
	}

	return capVal, nil
}
