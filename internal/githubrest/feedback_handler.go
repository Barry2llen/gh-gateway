package githubrest

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"gh-gateway/internal/feedback"
)

type ConversationCommentsService interface {
	ListConversationComments(context.Context, feedback.Query) ([]feedback.ConversationComment, error)
}

type ReviewsService interface {
	ListReviews(context.Context, feedback.Query) ([]feedback.Review, error)
}

type InlineCommentsService interface {
	ListInlineComments(context.Context, feedback.Query) ([]feedback.InlineComment, error)
}

type conversationCommentsHandler struct{ service ConversationCommentsService }

type actorResponse struct {
	Login string `json:"login"`
}

type conversationCommentResponse struct {
	ID                int64                `json:"id"`
	User              actorResponse        `json:"user"`
	AuthorAssociation feedback.Association `json:"author_association"`
	CreatedAt         string               `json:"created_at"`
	Body              string               `json:"body"`
	HTMLURL           string               `json:"html_url"`
}

func NewConversationCommentsHandler(service ConversationCommentsService) http.Handler {
	return &conversationCommentsHandler{service: service}
}

type reviewsHandler struct{ service ReviewsService }
type reviewResponse struct {
	ID                int64                `json:"id"`
	User              actorResponse        `json:"user"`
	AuthorAssociation feedback.Association `json:"author_association"`
	State             feedback.ReviewState `json:"state"`
	SubmittedAt       string               `json:"submitted_at"`
	CreatedAt         string               `json:"created_at"`
	Body              string               `json:"body"`
	HTMLURL           string               `json:"html_url"`
}

func NewReviewsHandler(service ReviewsService) http.Handler { return &reviewsHandler{service: service} }
func (h *reviewsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	page, ok := parsePage(w, r)
	if !ok {
		return
	}
	number, ok := parseNumber(w, r)
	if !ok {
		return
	}
	reviews, err := h.service.ListReviews(r.Context(), feedback.Query{Owner: chi.URLParam(r, "owner"), Repo: chi.URLParam(r, "repo"), Number: number, Authorization: r.Header.Get("Authorization"), Page: page})
	if err != nil {
		writeFeedbackError(w, err)
		return
	}
	result := make([]reviewResponse, 0, len(reviews))
	for _, review := range reviews {
		createdAt := review.CreatedAt
		if createdAt.IsZero() {
			createdAt = review.SubmittedAt
		}
		result = append(result, reviewResponse{ID: review.ID, User: actorResponse{Login: review.Author.Login}, AuthorAssociation: review.AuthorAssociation, State: review.State, SubmittedAt: review.SubmittedAt.Format(time.RFC3339), CreatedAt: createdAt.Format(time.RFC3339), Body: review.Body, HTMLURL: review.HTMLURL})
	}
	writeJSON(w, http.StatusOK, result)
}

type inlineCommentsHandler struct{ service InlineCommentsService }

type inlineCommentResponse struct {
	ID                int64                `json:"id"`
	ReviewID          int64                `json:"pull_request_review_id"`
	User              actorResponse        `json:"user"`
	AuthorAssociation feedback.Association `json:"author_association"`
	CreatedAt         string               `json:"created_at"`
	Body              string               `json:"body"`
	Path              string               `json:"path"`
	Line              *uint64              `json:"line"`
	OriginalLine      *uint64              `json:"original_line"`
	HTMLURL           string               `json:"html_url"`
}

func NewInlineCommentsHandler(service InlineCommentsService) http.Handler {
	return &inlineCommentsHandler{service: service}
}

func (h *inlineCommentsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	page, ok := parsePage(w, r)
	if !ok {
		return
	}
	number, ok := parseNumber(w, r)
	if !ok {
		return
	}
	comments, err := h.service.ListInlineComments(r.Context(), feedback.Query{Owner: chi.URLParam(r, "owner"), Repo: chi.URLParam(r, "repo"), Number: number, Authorization: r.Header.Get("Authorization"), Page: page})
	if err != nil {
		writeFeedbackError(w, err)
		return
	}
	result := make([]inlineCommentResponse, 0, len(comments))
	for _, comment := range comments {
		var line, originalLine *uint64
		if comment.Line > 0 {
			value := comment.Line
			line = &value
		}
		if comment.OriginalLine > 0 {
			value := comment.OriginalLine
			originalLine = &value
		}
		result = append(result, inlineCommentResponse{ID: comment.ID, ReviewID: comment.ReviewID, User: actorResponse{Login: comment.Author.Login}, AuthorAssociation: comment.AuthorAssociation, CreatedAt: comment.CreatedAt.Format(time.RFC3339), Body: comment.Body, Path: comment.Path, Line: line, OriginalLine: originalLine, HTMLURL: comment.HTMLURL})
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *conversationCommentsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	page, ok := parsePage(w, r)
	if !ok {
		return
	}
	number, ok := parseNumber(w, r)
	if !ok {
		return
	}
	comments, err := h.service.ListConversationComments(r.Context(), feedback.Query{
		Owner: chi.URLParam(r, "owner"), Repo: chi.URLParam(r, "repo"), Number: number,
		Authorization: r.Header.Get("Authorization"), Page: page,
	})
	if err != nil {
		writeFeedbackError(w, err)
		return
	}
	result := make([]conversationCommentResponse, 0, len(comments))
	for _, comment := range comments {
		result = append(result, conversationCommentResponse{ID: comment.ID, User: actorResponse{Login: comment.Author.Login}, AuthorAssociation: comment.AuthorAssociation, CreatedAt: comment.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), Body: comment.Body, HTMLURL: comment.HTMLURL})
	}
	writeJSON(w, http.StatusOK, result)
}

func parseNumber(w http.ResponseWriter, r *http.Request) (int64, bool) {
	number, err := strconv.ParseInt(chi.URLParam(r, "number"), 10, 64)
	if err != nil || number <= 0 {
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Message: "Validation Failed"})
		return 0, false
	}
	return number, true
}

func parsePage(w http.ResponseWriter, r *http.Request) (feedback.Page, bool) {
	page, perPage := 1, 30
	var err error
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, err = strconv.Atoi(raw)
		if err != nil || page <= 0 {
			writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Message: "Validation Failed"})
			return feedback.Page{}, false
		}
	}
	if raw := r.URL.Query().Get("per_page"); raw != "" {
		perPage, err = strconv.Atoi(raw)
		if err != nil || perPage <= 0 || perPage > 100 {
			writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Message: "Validation Failed"})
			return feedback.Page{}, false
		}
	}
	return feedback.Page{Number: page, PerPage: perPage}, true
}

func writeFeedbackError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, feedback.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Message: "Not Found"})
	case errors.Is(err, feedback.ErrUnauthorized):
		writeJSON(w, http.StatusUnauthorized, errorResponse{Message: "Requires authentication."})
	case errors.Is(err, feedback.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorResponse{Message: "Resource not accessible with the supplied credentials."})
	default:
		writeJSON(w, http.StatusBadGateway, errorResponse{Message: "The upstream service could not be reached."})
	}
}
