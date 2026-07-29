package exportformat

import (
	"fmt"
	"sort"
	"strings"
)

type Inspection struct {
	SourceFiles                      []string
	Conversations                    int64
	Messages                         int64
	MessagesWithSourceID             int64
	MessagesWithoutSourceID          int64
	UniqueSourceMessageIDs           int64
	RepeatedSourceMessageIDs         int64
	RepeatedSourceMessageOccurrences int64
	ArchivedConversations            int64
	ArchiveStatusKnown               int64
	Roles                            map[string]int64
	ContentTypes                     map[string]int64
}

func InspectSources(collection *SourceCollection) (Inspection, error) {
	inspection := Inspection{
		SourceFiles:  make([]string, 0, len(collection.Sources)),
		Roles:        make(map[string]int64),
		ContentTypes: make(map[string]int64),
	}
	sourceMessageIDCounts := make(map[string]int64)

	for _, source := range collection.Sources {
		inspection.SourceFiles = append(inspection.SourceFiles, source.Name)
		sourceReader, openError := source.Open()
		if openError != nil {
			return inspection, fmt.Errorf("open %s: %w", source.Name, openError)
		}
		streamError := StreamConversations(sourceReader, func(rawConversation RawConversation) error {
			inspection.Conversations++
			archiveValue := rawConversation.IsArchived
			if archiveValue == nil {
				archiveValue = rawConversation.Archived
			}
			if archiveValue != nil {
				inspection.ArchiveStatusKnown++
				if *archiveValue {
					inspection.ArchivedConversations++
				}
			}

			for _, rawNode := range rawConversation.Mapping {
				if rawNode.Message == nil {
					continue
				}
				inspection.Messages++
				sourceMessageID := strings.TrimSpace(rawNode.Message.ID)
				if sourceMessageID == "" {
					inspection.MessagesWithoutSourceID++
				} else {
					inspection.MessagesWithSourceID++
					priorCount := sourceMessageIDCounts[sourceMessageID]
					if priorCount == 1 {
						inspection.RepeatedSourceMessageIDs++
					}
					if priorCount > 0 {
						inspection.RepeatedSourceMessageOccurrences++
					}
					sourceMessageIDCounts[sourceMessageID] = priorCount + 1
				}
				role := rawNode.Message.Author.Role
				if role == "" {
					role = "unknown"
				}
				inspection.Roles[role]++
				extractedContent := ExtractContent(rawNode.Message.Content)
				contentType := extractedContent.ContentType
				if contentType == "" {
					contentType = "unknown"
				}
				inspection.ContentTypes[contentType]++
			}
			return nil
		})
		closeError := sourceReader.Close()
		if streamError != nil {
			return inspection, fmt.Errorf("inspect %s: %w", source.Name, streamError)
		}
		if closeError != nil {
			return inspection, fmt.Errorf("close %s: %w", source.Name, closeError)
		}
	}
	inspection.UniqueSourceMessageIDs = int64(len(sourceMessageIDCounts))

	sort.Strings(inspection.SourceFiles)
	return inspection, nil
}

func SortedCountKeys(values map[string]int64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
