package githubrest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"gh-gateway/internal/feedback"
)

type feedbackServiceStub struct {
	comments       []feedback.ConversationComment
	reviews        []feedback.Review
	inlineComments []feedback.InlineComment
	err            error
	query          feedback.Query
}

func (s *feedbackServiceStub) ListReviews(_ context.Context, query feedback.Query) ([]feedback.Review, error) {
	s.query = query
	return s.reviews, s.err
}
func (s *feedbackServiceStub) ListInlineComments(_ context.Context, query feedback.Query) ([]feedback.InlineComment, error) {
	s.query = query
	return s.inlineComments, s.err
}

func restTestRouter(handlers Handlers) http.Handler {
	router := chi.NewRouter()
	router.Mount("/api/v3", NewRouter(handlers))
	return router
}

func (s *feedbackServiceStub) ListConversationComments(_ context.Context, query feedback.Query) ([]feedback.ConversationComment, error) {
	s.query = query
	return s.comments, s.err
}

func TestConversationCommentsHandler(t *testing.T) {
	t.Parallel()

	service := &feedbackServiceStub{comments: []feedback.ConversationComment{{
		ID: 7, Author: feedback.Actor{Login: "reviewer"}, AuthorAssociation: feedback.AssociationCollaborator,
		CreatedAt: time.Date(2026, 9, 15, 1, 2, 3, 0, time.UTC), Body: "hello", HTMLURL: "https://git.example.test/foo/bar/issues/12#issuecomment-7",
	}}}
	router := restTestRouter(Handlers{ConversationComments: NewConversationCommentsHandler(service)})
	request := httptest.NewRequest(http.MethodGet, "/api/v3/repos/foo/bar/issues/12/comments?per_page=100&page=2", nil)
	request.Header.Set("Authorization", "token incoming")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"author_association":"COLLABORATOR"`) {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}
	if service.query.Owner != "foo" || service.query.Repo != "bar" || service.query.Number != 12 || service.query.Page.Number != 2 || service.query.Page.PerPage != 100 || service.query.Authorization != "token incoming" {
		t.Fatalf("query = %#v", service.query)
	}
}

func TestConversationCommentsHandlerValidatesPaginationAndSanitizesErrors(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v3/repos/foo/bar/issues/12/comments?page=0", "/api/v3/repos/foo/bar/issues/12/comments?per_page=101",
	} {
		response := httptest.NewRecorder()
		restTestRouter(Handlers{ConversationComments: NewConversationCommentsHandler(&feedbackServiceStub{})}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusUnprocessableEntity || strings.TrimSpace(response.Body.String()) != `{"message":"Validation Failed"}` {
			t.Fatalf("%s status/body = %d/%s", path, response.Code, response.Body.String())
		}
	}

	response := httptest.NewRecorder()
	restTestRouter(Handlers{ConversationComments: NewConversationCommentsHandler(&feedbackServiceStub{err: errors.New("gitea secret")})}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v3/repos/foo/bar/issues/12/comments", nil))
	if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "gitea") {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}
}

func TestFeedbackHandlersMapStableRESTErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		err     error
		status  int
		message string
	}{{feedback.ErrNotFound, 404, "Not Found"}, {feedback.ErrUnauthorized, 401, "Requires authentication."}, {feedback.ErrForbidden, 403, "Resource not accessible with the supplied credentials."}} {
		response := httptest.NewRecorder()
		restTestRouter(Handlers{ConversationComments: NewConversationCommentsHandler(&feedbackServiceStub{err: tt.err})}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v3/repos/foo/bar/issues/12/comments", nil))
		if response.Code != tt.status || !strings.Contains(response.Body.String(), tt.message) {
			t.Fatalf("error %v status/body = %d/%s", tt.err, response.Code, response.Body.String())
		}
	}
}

func TestReviewsHandlerPreservesPending(t *testing.T) {
	t.Parallel()
	service := &feedbackServiceStub{reviews: []feedback.Review{{ID: 9, Author: feedback.Actor{Login: "reviewer"}, AuthorAssociation: feedback.AssociationCollaborator, State: feedback.ReviewPending, SubmittedAt: time.Date(2026, 9, 15, 1, 2, 3, 0, time.UTC), Body: "draft", HTMLURL: "https://example/reviews/9"}}}
	response := httptest.NewRecorder()
	restTestRouter(Handlers{Reviews: NewReviewsHandler(service)}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v3/repos/foo/bar/pulls/12/reviews?per_page=100&page=1", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"PENDING"`) || !strings.Contains(response.Body.String(), `"created_at":"2026-09-15T01:02:03Z"`) {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}
}

func TestInlineCommentsHandlerUsesNullableLineFields(t *testing.T) {
	t.Parallel()
	service := &feedbackServiceStub{inlineComments: []feedback.InlineComment{{
		ID: 4, ReviewID: 9, Author: feedback.Actor{Login: "reviewer"}, AuthorAssociation: feedback.AssociationCollaborator,
		CreatedAt: time.Date(2026, 9, 15, 1, 2, 3, 0, time.UTC), Body: "inline", Path: "x.go", OriginalLine: 3, HTMLURL: "https://example/comment/4",
	}}}
	response := httptest.NewRecorder()
	restTestRouter(Handlers{InlineComments: NewInlineCommentsHandler(service)}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v3/repos/foo/bar/pulls/12/comments?per_page=100&page=1", nil))
	body := response.Body.String()
	if response.Code != 200 || !strings.Contains(body, `"pull_request_review_id":9`) || !strings.Contains(body, `"line":null`) || !strings.Contains(body, `"original_line":3`) {
		t.Fatalf("status/body = %d/%s", response.Code, body)
	}
}
