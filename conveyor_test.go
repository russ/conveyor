package conveyor

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-github/github"

	"golang.org/x/net/context"
)

func TestConveyor_Build(t *testing.T) {
	b := func(ctx context.Context, w Logger, opts BuildOptions) (string, error) {
		return "", nil
	}
	w := &mockLogger{}
	c := New(BuilderFunc(b))

	if _, err := c.Build(context.Background(), w, BuildOptions{}); err != nil {
		t.Fatal(err)
	}

	if !w.closed {
		t.Fatal("Expected logger to be closed")
	}
}

func TestConveyor_Build_CloseError(t *testing.T) {
	closeErr := errors.New("i/o timeout")
	b := func(ctx context.Context, w Logger, opts BuildOptions) (string, error) {
		return "", nil
	}
	w := &mockLogger{closeErr: closeErr}
	c := New(BuilderFunc(b))

	if _, err := c.Build(context.Background(), w, BuildOptions{}); err != closeErr {
		t.Fatalf("Expected error to be %v", closeErr)
	}

	if !w.closed {
		t.Fatal("Expected logger to be closed")
	}
}

func TestUpdateGitHubCommitStatus(t *testing.T) {
	b := func(ctx context.Context, w Logger, opts BuildOptions) (string, error) {
		return "", nil
	}
	g := &MockGitHubClient{}
	w := &mockLogger{}
	builder := UpdateGitHubCommitStatus(BuilderFunc(b), g)
	builder.since = func(t time.Time) time.Duration {
		return time.Second
	}

	g.On("CreateStatus", "remind101", "acme-inc", "abcd", &github.RepoStatus{
		State:       github.String("pending"),
		Description: github.String("Image building."),
		Context:     github.String("container/docker"),
	}).Return(nil)
	g.On("CreateStatus", "remind101", "acme-inc", "abcd", &github.RepoStatus{
		State:       github.String("success"),
		Description: github.String("Image built in 1s."),
		TargetURL:   github.String("https://google.com"),
		Context:     github.String("container/docker"),
	}).Return(nil)

	builder.Build(context.Background(), w, BuildOptions{
		Repository: "remind101/acme-inc",
		Branch:     "master",
		Sha:        "abcd",
	})

	g.AssertExpectations(t)
}

func TestUpdateGitHubCommitStatus_Error(t *testing.T) {
	b := func(ctx context.Context, w Logger, opts BuildOptions) (string, error) {
		return "", errors.New("i/o timeout")
	}
	g := &MockGitHubClient{}
	w := &mockLogger{}
	builder := UpdateGitHubCommitStatus(BuilderFunc(b), g)
	builder.since = func(t time.Time) time.Duration {
		return time.Second
	}

	g.On("CreateStatus", "remind101", "acme-inc", "abcd", &github.RepoStatus{
		State:       github.String("pending"),
		Description: github.String("Image building."),
		Context:     github.String("container/docker"),
	}).Return(nil)
	g.On("CreateStatus", "remind101", "acme-inc", "abcd", &github.RepoStatus{
		State:       github.String("failure"),
		Description: github.String("i/o timeout"),
		TargetURL:   github.String("https://google.com"),
		Context:     github.String("container/docker"),
	}).Return(nil)

	builder.Build(context.Background(), w, BuildOptions{
		Repository: "remind101/acme-inc",
		Branch:     "master",
		Sha:        "abcd",
	})

	g.AssertExpectations(t)
}

func TestWithCancel(t *testing.T) {
	var (
		// Total number of builds to add.
		numBuilds   = 2
		numCanceled int

		// context.Contexts are sent onto this channel when the build
		// starts.
		building = make(chan context.Context, numBuilds)
		canceled = make(chan context.Context, numBuilds)
	)

	b := WithCancel(BuilderFunc(func(ctx context.Context, w Logger, opts BuildOptions) (string, error) {
		building <- ctx

		select {
		case <-time.After(1 * time.Minute):
			t.Fatal("Got here")
		case <-ctx.Done():
			if ctx.Err() != context.Canceled {
				t.Fatal("Expected to be canceled")
				return "", nil
			}

			canceled <- ctx
			numCanceled += 1

			if numCanceled == numBuilds {
				close(canceled)
			}
		}

		return "", nil
	}))
	w := &mockLogger{}

	// Add a couple builds.
	for i := 0; i < numBuilds; i++ {
		go func() {
			b.Build(context.Background(), w, BuildOptions{
				Repository: "remind101/acme-inc",
				Branch:     "master",
				Sha:        "abcd",
			})
		}()

		// Wait for the build to start.
		<-building
	}

	if err := b.Cancel(); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		for range canceled {
			// Wait for builds to cancel
		}
		close(done)
	}()

	select {
	case <-done:
		// Yay
	case <-time.After(time.Second):
		t.Fatal("Timedout waiting for builds to cancel")
	}

	if got, want := numCanceled, numBuilds; got != want {
		t.Fatalf("%d builds canceled; want %d", got, want)
	}
}

type mockLogger struct {
	closeErr error
	closed   bool
}

func (m *mockLogger) Write(p []byte) (int, error) {
	return len(p), nil
}

func (m *mockLogger) Close() error {
	m.closed = true
	return m.closeErr
}

func (m *mockLogger) URL() string {
	return "https://google.com"
}
