package capacity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Helper function to create a temporary test file with JSON data.
func createTestProfileFile(t *testing.T, profileData CapacityProfileData) string {
	t.Helper()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test_profile.json")

	data, err := json.Marshal(profileData)
	if err != nil {
		t.Fatalf("failed to marshal profile data: %v", err)
	}

	err = os.WriteFile(filePath, data, 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	return filePath
}

func TestLoadProfile(t *testing.T) {
	// Test case 1: Successful load of a valid JSON file.
	t.Run("success", func(t *testing.T) {
		testData := CapacityProfileData{
			InputBuckets:  []int{10, 20},
			OutputBuckets: []int{5, 15},
			CapacityMap:   map[string]float64{"10input_5output": 100},
		}
		filePath := createTestProfileFile(t, testData)

		profile, err := LoadProfile(filePath)
		if err != nil {
			t.Fatalf("loadProfile failed unexpectedly: %v", err)
		}

		if len(profile.InputBuckets) != 2 || profile.InputBuckets[0] != 10 {
			t.Errorf("InputBuckets loaded incorrectly: %v", profile.InputBuckets)
		}
		if len(profile.OutputBuckets) != 2 || profile.OutputBuckets[0] != 5 {
			t.Errorf("OutputBuckets loaded incorrectly: %v", profile.OutputBuckets)
		}
		if cap, ok := profile.CapacityMap["10input_5output"]; !ok || cap != 100 {
			t.Errorf("CapacityMap loaded incorrectly: %v", profile.CapacityMap)
		}
	})

	// Test case 2: File not found error.
	t.Run("file not found", func(t *testing.T) {
		_, err := LoadProfile("nonexistent.json")
		if err == nil {
			t.Error("expected an error for a non-existent file, but got nil")
		}
	})

	// Test case 3: Invalid JSON format.
	t.Run("invalid json", func(t *testing.T) {
		filePath := filepath.Join(t.TempDir(), "invalid.json")
		err := os.WriteFile(filePath, []byte(`{"input_buckets": "not an array"}`), 0644)
		if err != nil {
			t.Fatalf("failed to create invalid JSON file: %v", err)
		}

		_, err = LoadProfile(filePath)
		if err == nil {
			t.Error("expected an error for invalid JSON, but got nil")
		}
	})
}

func TestLocateBucket(t *testing.T) {
	buckets := []int{10, 50, 100}

	testCases := []struct {
		name   string
		target int
		want   int
	}{
		{"target less than first bucket", 5, 10},
		{"target between buckets", 45, 50},
		{"target is a bucket value", 50, 50},
		{"target greater than last bucket", 120, 100},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := locateBucket(tc.target, buckets)
			if got != tc.want {
				t.Errorf("locateBucket(%d, %v) got %d, want %d", tc.target, buckets, got, tc.want)
			}
		})
	}
}

func TestLocateRandomErrorBucket(t *testing.T) {
	buckets := []int{10, 50, 100}
	correctBucket := 50
	numRuns := 100 // Run multiple times to ensure the result is random but not the correct one.

	for i := 0; i < numRuns; i++ {
		errorBucket := locateRandomErrorBucket(correctBucket, buckets)
		if errorBucket == correctBucket {
			t.Errorf("locateRandomErrorBucket returned the correct bucket (%d)", correctBucket)
		}
	}
}

func TestPredictRequestType(t *testing.T) {
	profile := &CapacityProfileData{
		InputBuckets:  []int{10, 50, 100},
		OutputBuckets: []int{20, 60, 120},
	}

	t.Run("no accuracy env var set", func(t *testing.T) {
		os.Unsetenv("OUTPUT_PREDICTOR_ACCURACY")
		requestType, err := profile.PredictRequestType(15, 70)
		if err != nil {
			t.Fatalf("PredictRequestType returned an unexpected error: %v", err)
		}
		if !requestType.Valid {
			t.Errorf("PredictRequestType got Valid=false, want true when no env var is set")
		}
	})

	t.Run("accuracy 1.0", func(t *testing.T) {
		os.Setenv("OUTPUT_PREDICTOR_ACCURACY", "1")
		defer os.Unsetenv("OUTPUT_PREDICTOR_ACCURACY")

		requestType, err := profile.PredictRequestType(15, 70)
		if err != nil {
			t.Fatalf("PredictRequestType returned an unexpected error: %v", err)
		}
		if !requestType.Valid {
			t.Errorf("PredictRequestType got Valid=false, want true with 1.0 accuracy")
		}
	})

	t.Run("50% accuracy simulation", func(t *testing.T) {
		os.Setenv("OUTPUT_PREDICTOR_ACCURACY", "0.5")
		defer os.Unsetenv("OUTPUT_PREDICTOR_ACCURACY")
		
		numSimulations := 10000
		inputLength := 30
		gtOutputLength := 80
		accuracy := 0.5
		correctCount := 0
		
		for i := 0; i < numSimulations; i++ {
			reqType, err := profile.PredictRequestType(inputLength, gtOutputLength)
			if err != nil {
				t.Fatalf("simulation failed: %v", err)
			}
			if reqType.Valid {
				correctCount++
			}
		}

		correctRatio := float64(correctCount) / float64(numSimulations)
		tolerance := 0.05
		if !(correctRatio >= accuracy-tolerance && correctRatio <= accuracy+tolerance) {
			t.Errorf("Incorrect correct ratio. Got %f, expected around %f", correctRatio, accuracy)
		}
	})

	t.Run("invalid accuracy value", func(t *testing.T) {
		os.Setenv("OUTPUT_PREDICTOR_ACCURACY", "not-a-number")
		defer os.Unsetenv("OUTPUT_PREDICTOR_ACCURACY")

		_, err := profile.PredictRequestType(15, 70)
		if err == nil {
			t.Errorf("Expected an error for invalid accuracy value, but got nil")
		}
	})
}

func TestObtainCapacity(t *testing.T) {
	profile := &CapacityProfileData{
		CapacityMap: map[string]float64{
			"10input_20output": 100,
			"50input_60output": 250,
		},
	}

	testCases := []struct {
		name      string
		key       string
		wantCap   float64
		wantErr   bool
	}{
		{"valid key", "10input_20output", 100, false},
		{"another valid key", "50input_60output", 250, false},
		{"invalid key", "nonexistent_key", 0, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotCap, err := profile.ObtainCapacity(tc.key)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ObtainCapacity() error mismatch. got error %v, want error %t", err, tc.wantErr)
			}
			if !tc.wantErr && gotCap != tc.wantCap {
				t.Errorf("ObtainCapacity() got capacity %f, want %f", gotCap, tc.wantCap)
			}
		})
	}
}