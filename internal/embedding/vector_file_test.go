package embedding

import (
	"path/filepath"
	"testing"
)

func TestVectorFileAppendReadAndRepair(testContext *testing.T) {
	vectorPath := filepath.Join(testContext.TempDir(), "vectors.f32")
	vectorFile, openError := OpenVectorFile(vectorPath, 3, -1)
	if openError != nil {
		testContext.Fatalf("open vector file: %v", openError)
	}
	rows, appendError := vectorFile.Append([][]float32{{1, 0, 0}, {0, 1, 0}})
	if appendError != nil {
		testContext.Fatalf("append vectors: %v", appendError)
	}
	if len(rows) != 2 || rows[0] != 0 || rows[1] != 1 {
		testContext.Fatalf("unexpected rows: %v", rows)
	}
	readVector, readError := vectorFile.Read(1)
	if readError != nil {
		testContext.Fatalf("read vector: %v", readError)
	}
	if readVector[0] != 0 || readVector[1] != 1 || readVector[2] != 0 {
		testContext.Fatalf("unexpected vector: %v", readVector)
	}
	if closeError := vectorFile.Close(); closeError != nil {
		testContext.Fatalf("close vector file: %v", closeError)
	}

	repairedFile, repairError := OpenVectorFile(vectorPath, 3, 0)
	if repairError != nil {
		testContext.Fatalf("repair vector file: %v", repairError)
	}
	defer repairedFile.Close()
	if _, readError := repairedFile.Read(1); readError == nil {
		testContext.Fatal("orphaned second row should have been truncated")
	}
}

func TestScanVectorFileVisitsEveryContiguousRow(testContext *testing.T) {
	vectorPath := filepath.Join(testContext.TempDir(), "scan.f32")
	vectorFile, openError := OpenVectorFile(vectorPath, 3, -1)
	if openError != nil {
		testContext.Fatalf("open vector file: %v", openError)
	}
	if _, appendError := vectorFile.Append([][]float32{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}); appendError != nil {
		vectorFile.Close()
		testContext.Fatalf("append scan vectors: %v", appendError)
	}
	if closeError := vectorFile.Close(); closeError != nil {
		testContext.Fatalf("close vector file: %v", closeError)
	}

	visited := make([][]float32, 0)
	if scanError := ScanVectorFile(vectorPath, 3, func(row int64, vector []float32) error {
		if row != int64(len(visited)) {
			testContext.Fatalf("unexpected scan row %d", row)
		}
		visited = append(visited, append([]float32(nil), vector...))
		return nil
	}); scanError != nil {
		testContext.Fatalf("scan vector file: %v", scanError)
	}
	if len(visited) != 3 || visited[1][1] != 1 {
		testContext.Fatalf("unexpected scanned vectors: %+v", visited)
	}
}
