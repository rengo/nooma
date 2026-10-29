package httpapi

import (
	"net/http"
	"time"
)

// learnedValueResponse is one learned value as GET /learning returns it.
type learnedValueResponse struct {
	Kind       string     `json:"kind"`
	Key        string     `json:"key"`
	Value      string     `json:"value"`
	Pinned     bool       `json:"pinned"`
	Sentence   string     `json:"sentence"`
	At         *time.Time `json:"at"`
	CanUndo    bool       `json:"can_undo"`
	CanRelease bool       `json:"can_release"`
}

// learningHandler answers GET /learning: the summary of what was learned, one
// entry per value (doc 02 §9). It writes nothing; undo and release are the
// UI's (/ui/learning).
func learningHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Learning == nil {
			writeError(w, http.StatusServiceUnavailable, "not_wired", "learning is not wired in this build")
			return
		}
		view, err := d.Learning.View(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "reading what was learned failed")
			return
		}
		values := make([]learnedValueResponse, len(view.Values))
		for i, v := range view.Values {
			values[i] = learnedValueResponse{Kind: string(v.Kind), Key: v.Key, Value: v.Value, Pinned: v.Pinned,
				Sentence: v.Sentence, At: v.At, CanUndo: v.CanUndo, CanRelease: v.CanRelease}
		}
		writeJSON(w, http.StatusOK, map[string]any{"values": values})
	}
}
