package vision

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
)

// ProcessResult contains the outcome of processing a payload through the registry.
type ProcessResult struct {
	Payload        []byte       // modified payload with placeholders
	HasNewImages   bool         // current turn has new images (let fallback handle)
	ImagesFound    int          // total unique images found
	HistoricalOnly bool         // only historical images, no new ones
	RegistryNote   string       // generated registry note (may be empty)
}

// Processor orchestrates the vision registry workflow for a single request.
type Processor struct {
	registry   *GlobalRegistry
	analyzer   ImageAnalyzer
	maxEntries int
}

// NewProcessor creates a Processor bound to the global registry.
func NewProcessor(analyzer ImageAnalyzer) *Processor {
	return &Processor{
		registry:   GetGlobal(),
		analyzer:   analyzer,
		maxEntries: DefaultGlobalConfig().MaxEntriesPerSession,
	}
}

// Process handles all image-related concerns for a single request payload.
// It should be called early in the executor's Execute/ExecuteStream path.
//
// The processor:
//  1. Walks the payload to identify all images (current and historical)
//  2. For current-turn images: does nothing (existing fallback handles them)
//  3. For historical images: replaces with short placeholders and may generate
//     a registry note for the current user message
//
// sessionKey may be empty — if so, cross-turn memory is disabled and only
// single-request processing occurs.
func (p *Processor) Process(ctx context.Context, payload []byte, sessionKey SessionKey, turnIndex int) (*ProcessResult, error) {
	result := &ProcessResult{
		Payload: payload,
	}

	walk := WalkPayload(payload)
	if len(walk.Parts) == 0 {
		return result, nil
	}

	result.ImagesFound = len(walk.Parts)

	// Check if we have a valid session for cross-turn memory
	hasSession := sessionKey != ""

	// Separate current-turn images from historical
	var currentParts []ImagePart
	var historicalParts []ImagePart
	for _, part := range walk.Parts {
		if part.IsCurrent {
			currentParts = append(currentParts, part)
		} else {
			historicalParts = append(historicalParts, part)
		}
	}

	result.HasNewImages = len(currentParts) > 0
	result.HistoricalOnly = len(currentParts) == 0 && len(historicalParts) > 0

	// Process current-turn images: record in registry if we have a session,
	// but don't replace them (let existing fallback handle current images).
	var sessionStore *SessionStore
	if hasSession {
		sessionStore = p.registry.GetOrCreateSession(sessionKey)
		for _, part := range currentParts {
			data, _ := ExtractImageData(&part)
			if data == "" {
				continue
			}
			hash := ComputeHash(data)
			sessionStore.GetOrCreateEntry(hash, 0, p.maxEntries)
			ordinal := len(sessionStore.AllEntries())

			sessionStore.UpdateEntry(hash, func(e *ImageEntry) {
				if e.StableOrdinal == 0 {
					e.StableOrdinal = ordinal
				}
				e.SourceKind = ImageSourceUserUpload
				e.FirstSeenTurn = turnIndex
				e.LastSeenTurn = turnIndex
				e.CurrentPayloadReachable = true
				e.Availability = ImageAvailableInline
				e.Occurrences = append(e.Occurrences, ImageOccurrence{
					TurnIndex:  turnIndex,
					MessageIdx: part.MsgIdx,
					PartIdx:    part.PartIdx,
				})
			})
		}
	}

	// Process historical images: replace with placeholders
	if len(historicalParts) > 0 {
		for _, part := range historicalParts {
			data, _ := ExtractImageData(&part)
			if data == "" {
				continue
			}
			hash := ComputeHash(data)

			entry := p.findOrCreateEntry(sessionStore, hash, data, turnIndex)
			placeholder := BuildShortPlaceholder(entry)

			var err error
			result.Payload, err = ReplaceImagePart(result.Payload, part, placeholder)
			if err != nil {
				log.Warnf("vision: replace image part: %v", err)
			}
		}

		// Generate registry note for the current user message
		if sessionStore != nil {
			entries := sessionStore.AllEntries()
			if len(entries) > 0 {
				// Check intent from the last user message
				lastMsg := extractLastUserText(walk)
				refNum := ExtractImageReference(lastMsg)

				if refNum > 0 {
					// User is asking about a specific image — try to re-analyze
					p.handleReAnalysis(ctx, sessionStore, refNum, lastMsg, result)
				} else {
					intent := DetectIntent(lastMsg, len(entries))
					switch intent {
					case IntentFollowUp:
						// User is asking about images generally
						result.RegistryNote = BuildRegistryNote(entries, 0)
					case IntentAmbiguous:
						result.RegistryNote = BuildAmbiguityNote(entries)
					}
				}

				// Inject the registry note
				if result.RegistryNote != "" {
					var err error
					result.Payload, err = InjectRegistryNote(result.Payload, result.RegistryNote)
					if err != nil {
						log.Warnf("vision: inject registry note: %v", err)
					}
				}
			}
		}
	}

	return result, nil
}

func (p *Processor) findOrCreateEntry(sessionStore *SessionStore, hash ImageHash, data string, turnIndex int) *ImageEntry {
	if sessionStore == nil {
		// No session — create ephemeral entry for this request only
		return &ImageEntry{
			Hash:        hash,
			StableOrdinal: 0,
			Summary: ImageSummary{Confidence: "low"},
		}
	}

	entry := sessionStore.GetOrCreateEntry(hash, 0, p.maxEntries)
	if entry.StableOrdinal == 0 {
		all := sessionStore.AllEntries()
		entry.StableOrdinal = len(all)
	}

	sessionStore.UpdateEntry(hash, func(e *ImageEntry) {
		e.LastSeenTurn = turnIndex
		e.LastAccessAt = time.Now()
		e.Occurrences = append(e.Occurrences, ImageOccurrence{
			TurnIndex: turnIndex,
		})
	})

	return entry
}

func (p *Processor) handleReAnalysis(ctx context.Context, sessionStore *SessionStore, targetOrdinal int, query string, result *ProcessResult) {
	entries := sessionStore.AllEntries()

	// Find the target entry
	var target *ImageEntry
	for _, e := range entries {
		if e.StableOrdinal == targetOrdinal {
			target = e
			break
		}
	}
	if target == nil {
		return
	}

	if !target.CurrentPayloadReachable {
		// Can't re-analyze, use cached summary
		result.RegistryNote = BuildRegistryNote([]*ImageEntry{target}, targetOrdinal)
		return
	}

	// Re-analyze with the new question
	req := AnalyzeRequest{
		Existing:   target.Summary,
		Query:      query,
		IsFollowUp: target.Summary.Summary != "",
		SourceKind: target.SourceKind,
	}

	// Try to find the raw data in the current payload
	// (already processed — we need to extract from the walk result)
	walk := WalkPayload(result.Payload)
	for _, part := range walk.Parts {
		if !part.IsCurrent {
			data, mime := ExtractImageData(&part)
			if data != "" {
				req.ImageData = data
				req.MIMEType = mime
				break
			}
		}
	}

	if req.ImageData == "" {
		// Raw data not available
		result.RegistryNote = BuildRegistryNote([]*ImageEntry{target}, targetOrdinal)
		return
	}

	// Call the analyzer
	resp, err := p.analyzer.Analyze(ctx, req)
	if err != nil {
		log.Warnf("vision: re-analysis failed for Image #%d: %v", targetOrdinal, err)
		result.RegistryNote = BuildRegistryNote([]*ImageEntry{target}, targetOrdinal)
		return
	}

	// Update the entry with new details
	sessionStore.UpdateEntry(target.Hash, func(e *ImageEntry) {
		e.Summary = resp.Summary
		e.LastAnalyzedAt = time.Now()
	})

	// Build the registry note with the updated summary
	result.RegistryNote = BuildRegistryNote([]*ImageEntry{target}, targetOrdinal)
}

// extractLastUserText finds the text content of the last user message.
func extractLastUserText(walk *WalkResult) string {
	for i := len(walk.Parts) - 1; i >= 0; i-- {
		if walk.Parts[i].IsCurrent {
			// The current parts list doesn't contain text — we need to walk the
			// original payload. But for intent detection, we just return empty
			// and let the caller use the raw last message text.
			return ""
		}
	}
	return ""
}

// CurrentTurnHasImages is a helper for executors to quickly check if the
// current user message contains images (for fallback decisions).
func CurrentTurnHasImages(payload []byte) bool {
	walk := WalkPayload(payload)
	for _, p := range walk.Parts {
		if p.IsCurrent {
			return true
		}
	}
	return false
}
