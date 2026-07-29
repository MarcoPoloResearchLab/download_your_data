package embedding

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/MarcoPoloResearchLab/download_your_data/internal/privatepath"
)

type VectorFile struct {
	file       *os.File
	dimensions int
	rowBytes   int64
}

func OpenVectorFile(file privatepath.File, dimensions int, maximumCommittedRow int64) (*VectorFile, error) {
	if dimensions <= 0 {
		return nil, fmt.Errorf("vector dimensions must be positive")
	}
	if prepareError := file.Prepare(); prepareError != nil {
		return nil, fmt.Errorf("prepare private vector file: %w", prepareError)
	}
	vectorHandle, openError := os.OpenFile(file.Path(), os.O_RDWR, 0o600)
	if openError != nil {
		return nil, fmt.Errorf("open vector file: %w", openError)
	}
	vectorFile := &VectorFile{
		file:       vectorHandle,
		dimensions: dimensions,
		rowBytes:   int64(dimensions * 4),
	}
	if repairError := vectorFile.repair(maximumCommittedRow); repairError != nil {
		vectorHandle.Close()
		return nil, repairError
	}
	return vectorFile, nil
}

func (vectorFile *VectorFile) Close() error {
	return vectorFile.file.Close()
}

func (vectorFile *VectorFile) Append(vectors [][]float32) ([]int64, error) {
	fileInfo, statError := vectorFile.file.Stat()
	if statError != nil {
		return nil, fmt.Errorf("inspect vector file: %w", statError)
	}
	if fileInfo.Size()%vectorFile.rowBytes != 0 {
		return nil, fmt.Errorf("vector file size is not aligned to row size")
	}
	startingRow := fileInfo.Size() / vectorFile.rowBytes
	rows := make([]int64, len(vectors))
	buffer := make([]byte, int(vectorFile.rowBytes)*len(vectors))
	for vectorIndex, vector := range vectors {
		if len(vector) != vectorFile.dimensions {
			return nil, fmt.Errorf("vector %d has %d dimensions; expected %d", vectorIndex, len(vector), vectorFile.dimensions)
		}
		rows[vectorIndex] = startingRow + int64(vectorIndex)
		rowOffset := vectorIndex * int(vectorFile.rowBytes)
		for dimensionIndex, vectorValue := range vector {
			byteOffset := rowOffset + dimensionIndex*4
			binary.LittleEndian.PutUint32(buffer[byteOffset:], math.Float32bits(vectorValue))
		}
	}
	if _, seekError := vectorFile.file.Seek(0, io.SeekEnd); seekError != nil {
		return nil, fmt.Errorf("seek vector file: %w", seekError)
	}
	if _, writeError := vectorFile.file.Write(buffer); writeError != nil {
		return nil, fmt.Errorf("append vector file: %w", writeError)
	}
	if syncError := vectorFile.file.Sync(); syncError != nil {
		return nil, fmt.Errorf("flush vector file: %w", syncError)
	}
	return rows, nil
}

func (vectorFile *VectorFile) Read(row int64) ([]float32, error) {
	if row < 0 {
		return nil, fmt.Errorf("vector row must not be negative")
	}
	buffer := make([]byte, vectorFile.rowBytes)
	bytesRead, readError := vectorFile.file.ReadAt(buffer, row*vectorFile.rowBytes)
	if readError != nil && readError != io.EOF {
		return nil, fmt.Errorf("read vector row %d: %w", row, readError)
	}
	if int64(bytesRead) != vectorFile.rowBytes {
		return nil, fmt.Errorf("vector row %d is incomplete", row)
	}
	vector := make([]float32, vectorFile.dimensions)
	for dimensionIndex := 0; dimensionIndex < vectorFile.dimensions; dimensionIndex++ {
		vector[dimensionIndex] = math.Float32frombits(binary.LittleEndian.Uint32(buffer[dimensionIndex*4:]))
	}
	return vector, nil
}

func (vectorFile *VectorFile) repair(maximumCommittedRow int64) error {
	fileInfo, statError := vectorFile.file.Stat()
	if statError != nil {
		return fmt.Errorf("inspect vector file for repair: %w", statError)
	}
	expectedSize := int64(0)
	if maximumCommittedRow >= 0 {
		expectedSize = (maximumCommittedRow + 1) * vectorFile.rowBytes
	}
	if fileInfo.Size() < expectedSize {
		return fmt.Errorf("vector file is shorter than committed database rows: expected at least %d bytes, found %d", expectedSize, fileInfo.Size())
	}
	if fileInfo.Size() > expectedSize {
		if truncateError := vectorFile.file.Truncate(expectedSize); truncateError != nil {
			return fmt.Errorf("truncate orphaned vector bytes: %w", truncateError)
		}
	}
	return nil
}

func DotProduct(leftVector []float32, rightVector []float32) (float64, error) {
	if len(leftVector) != len(rightVector) {
		return 0, fmt.Errorf("vector length mismatch: %d and %d", len(leftVector), len(rightVector))
	}
	var dotProduct float64
	for dimensionIndex := range leftVector {
		dotProduct += float64(leftVector[dimensionIndex]) * float64(rightVector[dimensionIndex])
	}
	return dotProduct, nil
}

func ScanVectorFile(file privatepath.File, dimensions int, visit func(row int64, vector []float32) error) error {
	if dimensions <= 0 {
		return fmt.Errorf("vector dimensions must be positive")
	}
	vectorHandle, openError := os.Open(file.Path())
	if openError != nil {
		return fmt.Errorf("open vector file for scan: %w", openError)
	}
	defer vectorHandle.Close()

	rowBytes := dimensions * 4
	fileInfo, statError := vectorHandle.Stat()
	if statError != nil {
		return fmt.Errorf("inspect vector file for scan: %w", statError)
	}
	if fileInfo.Size()%int64(rowBytes) != 0 {
		return fmt.Errorf("vector file size is not aligned to %d-dimensional rows", dimensions)
	}

	reader := bufio.NewReaderSize(vectorHandle, rowBytes*256)
	buffer := make([]byte, rowBytes)
	vector := make([]float32, dimensions)
	rowCount := fileInfo.Size() / int64(rowBytes)
	for row := int64(0); row < rowCount; row++ {
		if _, readError := io.ReadFull(reader, buffer); readError != nil {
			return fmt.Errorf("read vector row %d during scan: %w", row, readError)
		}
		for dimensionIndex := range vector {
			vector[dimensionIndex] = math.Float32frombits(binary.LittleEndian.Uint32(buffer[dimensionIndex*4:]))
		}
		if visitError := visit(row, vector); visitError != nil {
			return visitError
		}
	}
	return nil
}
