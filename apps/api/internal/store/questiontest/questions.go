// Package questiontest checks message questions against each SQL backend.
package questiontest

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

// Hooks give the shared checks access to backend-specific SQL.
type Hooks struct {
	// ExpireQuestion moves a question's deadline into the past.
	ExpireQuestion func(t *testing.T, messageID string)
}

type fixture struct {
	owner     store.User
	member    store.User
	bot       store.User
	otherBot  store.User
	workspace store.Workspace
	channel   store.Channel
}

func QuestionLifecycle(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	f := newFixture(t, st)
	spec := questionSpec(f.member.ID)

	message, created, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: f.channel.ID,
		AuthorID:  f.bot.ID,
		Body:      "Agent needs input: which day do we ship?",
		Nonce:     "question-create-1",
		Question:  &spec,
	})
	if err != nil {
		t.Fatal(err)
	}
	question := message.Question
	if question == nil || question.Status != store.QuestionStatusOpen || question.Version != 1 || !question.AllowSkip || len(question.Items) != 2 || question.Items[0].Options[0].Label != "Lun 15 sep" {
		t.Fatalf("unexpected created question: %#v", question)
	}
	if !reflect.DeepEqual(created.MentionedUserIDs, []string{f.member.ID}) {
		t.Fatalf("responders must be mentioned, got %#v", created.MentionedUserIDs)
	}
	replayed, replayEvent, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.bot.ID, Body: message.Body, Nonce: "question-create-1", Question: &spec})
	if err != nil || replayed.ID != message.ID || replayEvent.ID != "" || replayed.Question == nil {
		t.Fatalf("nonce replay = %#v, %#v, %v", replayed, replayEvent, err)
	}
	changed := questionSpec(f.member.ID)
	changed.Items[0].Prompt = "A different question"
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.bot.ID, Body: message.Body, Nonce: "question-create-1", Question: &changed}); !errors.Is(err, store.ErrClientNonceConflict) {
		t.Fatalf("replay with a different question error = %v", err)
	}
	page, err := st.ListMessages(ctx, f.channel.ID, f.member.ID, store.MessagePageRequest{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if listed := messageByID(page.Messages, message.ID); listed == nil || listed.Question == nil || listed.Question.Status != store.QuestionStatusOpen {
		t.Fatalf("message page did not hydrate the question: %#v", listed)
	}

	answers := map[string][]string{"ship_date": {"Lun 15 sep"}, "boxes": {"120"}}
	if _, _, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: message.ID, UserID: f.owner.ID, Answers: answers}); !errors.Is(err, store.ErrQuestionResponderRequired) {
		t.Fatalf("non-responder answer error = %v", err)
	}
	if _, _, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: message.ID, UserID: f.member.ID, Answers: map[string][]string{"ship_date": {"Lun 15 sep"}}}); !errors.Is(err, store.ErrInvalidQuestion) {
		t.Fatalf("incomplete answer error = %v", err)
	}
	answered, events, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: message.ID, UserID: f.member.ID, Answers: answers, Nonce: "answer-1"})
	if err != nil {
		t.Fatal(err)
	}
	response := answered.Question.Response
	if answered.Question.Status != store.QuestionStatusSubmitted || answered.Question.Version != 2 || response == nil || response.Responder == nil || response.Responder.ID != f.member.ID || response.Source != store.QuestionResponseSourceClickClack || !reflect.DeepEqual(response.Answers, answers) {
		t.Fatalf("unexpected submitted question: %#v", answered.Question)
	}
	if len(events) != 2 || events[0].Type != "message.updated" || events[1].Type != "question.submitted" {
		t.Fatalf("unexpected answer events: %#v", events)
	}
	if payload, ok := events[1].Payload.(map[string]any); !ok || payload["responder_id"] != f.member.ID || payload["message_id"] != message.ID || payload["external_id"] != "ask_1" || payload["answers"] != nil {
		t.Fatalf("question.submitted must carry identifiers only: %#v", events[1].Payload)
	}
	if again, againEvents, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: message.ID, UserID: f.member.ID, Answers: answers, Nonce: "answer-1"}); err != nil || len(againEvents) != 0 || again.Question.Version != 2 {
		t.Fatalf("answer replay = %#v, %#v, %v", again.Question, againEvents, err)
	}
	if _, _, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: message.ID, UserID: f.member.ID, Answers: answers, Nonce: "answer-2"}); !errors.Is(err, store.ErrQuestionClosed) {
		t.Fatalf("second answer error = %v", err)
	}

	if _, _, err := st.ResolveQuestion(ctx, store.ResolveQuestionInput{MessageID: message.ID, BotUserID: f.otherBot.ID, Status: store.QuestionStatusAnswered}); !errors.Is(err, store.ErrQuestionAuthorRequired) {
		t.Fatalf("foreign bot resolve error = %v", err)
	}
	stale := int64(1)
	if _, _, err := st.ResolveQuestion(ctx, store.ResolveQuestionInput{MessageID: message.ID, BotUserID: f.bot.ID, Status: store.QuestionStatusAnswered, ExpectedVersion: &stale}); !errors.Is(err, store.ErrQuestionConflict) {
		t.Fatalf("stale version resolve error = %v", err)
	}
	current := int64(2)
	resolved, resolveEvent, err := st.ResolveQuestion(ctx, store.ResolveQuestionInput{MessageID: message.ID, BotUserID: f.bot.ID, Status: store.QuestionStatusAnswered, ExpectedVersion: &current})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Question.Status != store.QuestionStatusAnswered || resolved.Question.ResolvedAt == nil || resolved.Question.Version != 3 || resolved.Question.Response == nil || resolveEvent.Type != "message.updated" {
		t.Fatalf("unexpected resolution: %#v %#v", resolved.Question, resolveEvent)
	}
	if repeated, repeatedEvent, err := st.ResolveQuestion(ctx, store.ResolveQuestionInput{MessageID: message.ID, BotUserID: f.bot.ID, Status: store.QuestionStatusAnswered}); err != nil || repeatedEvent.ID != "" || repeated.Question.Version != 3 {
		t.Fatalf("repeated resolution = %#v, %#v, %v", repeated.Question, repeatedEvent, err)
	}
	if _, _, err := st.ResolveQuestion(ctx, store.ResolveQuestionInput{MessageID: message.ID, BotUserID: f.bot.ID, Status: store.QuestionStatusFailed}); !errors.Is(err, store.ErrQuestionConflict) {
		t.Fatalf("changing a terminal status error = %v", err)
	}
	unresolved, err := st.ListBotUnresolvedQuestions(ctx, f.workspace.ID, f.bot.ID, "", true, 50)
	if err != nil || len(unresolved) != 0 {
		t.Fatalf("answered questions must not be listed: %#v, %v", unresolved, err)
	}
}

func QuestionSkipReopenAndExternalAnswers(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	f := newFixture(t, st)
	ask := func(body string, mutate func(*store.QuestionSpec)) store.Message {
		t.Helper()
		spec := questionSpec()
		if mutate != nil {
			mutate(&spec)
		}
		message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.bot.ID, Body: body, Question: &spec})
		if err != nil {
			t.Fatal(err)
		}
		return message
	}

	skipped := ask("skip me", nil)
	submitted, _, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: skipped.ID, UserID: f.owner.ID, Skip: true})
	if err != nil || !submitted.Question.Response.Skipped || submitted.Question.Status != store.QuestionStatusSubmitted {
		t.Fatalf("skip = %#v, %v", submitted.Question, err)
	}
	if cancelled, _, err := st.ResolveQuestion(ctx, store.ResolveQuestionInput{MessageID: skipped.ID, BotUserID: f.bot.ID, Status: store.QuestionStatusCancelled}); err != nil || cancelled.Question.Status != store.QuestionStatusCancelled || !cancelled.Question.Response.Skipped {
		t.Fatalf("cancel after skip = %#v, %v", cancelled.Question, err)
	}

	required := ask("no skipping", func(spec *store.QuestionSpec) {
		allowSkip := false
		spec.AllowSkip = &allowSkip
	})
	if _, _, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: required.ID, UserID: f.member.ID, Skip: true}); !errors.Is(err, store.ErrInvalidQuestion) {
		t.Fatalf("skipping a required question error = %v", err)
	}

	reopened := ask("reopen me", nil)
	if _, _, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: reopened.ID, UserID: f.member.ID, Answers: map[string][]string{"ship_date": {"Otra fecha"}, "boxes": {"12"}}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ResolveQuestion(ctx, store.ResolveQuestionInput{MessageID: reopened.ID, BotUserID: f.bot.ID, Status: store.QuestionStatusOpen}); !errors.Is(err, store.ErrInvalidQuestion) {
		t.Fatalf("reopen without a note error = %v", err)
	}
	open, _, err := st.ResolveQuestion(ctx, store.ResolveQuestionInput{MessageID: reopened.ID, BotUserID: f.bot.ID, Status: store.QuestionStatusOpen, Note: "Pick a listed date"})
	if err != nil || open.Question.Status != store.QuestionStatusOpen || open.Question.Response != nil || open.Question.Note != "Pick a listed date" {
		t.Fatalf("reopened question = %#v, %v", open.Question, err)
	}
	if again, _, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: reopened.ID, UserID: f.owner.ID, Answers: map[string][]string{"ship_date": {"Mar 16 sep"}, "boxes": {"12"}}}); err != nil || again.Question.Response.Responder.ID != f.owner.ID || again.Question.Note != "" {
		t.Fatalf("answer after reopen = %#v, %v", again.Question, err)
	}

	elsewhere := ask("answered elsewhere", nil)
	external, _, err := st.ResolveQuestion(ctx, store.ResolveQuestionInput{MessageID: elsewhere.ID, BotUserID: f.bot.ID, Status: store.QuestionStatusAnswered, Note: "Answered elsewhere", Answers: map[string][]string{"ship_date": {"Mar 16 sep"}, "boxes": {"3"}}})
	if err != nil || external.Question.Response == nil || external.Question.Response.Source != store.QuestionResponseSourceExternal || external.Question.Response.Responder != nil {
		t.Fatalf("external answer = %#v, %v", external.Question, err)
	}
	if _, _, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: elsewhere.ID, UserID: f.member.ID, Answers: map[string][]string{"ship_date": {"Mar 16 sep"}, "boxes": {"3"}}}); !errors.Is(err, store.ErrQuestionClosed) {
		t.Fatalf("answering a resolved question error = %v", err)
	}

	unresolved, err := st.ListBotUnresolvedQuestions(ctx, f.workspace.ID, f.bot.ID, "", true, 50)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{}
	for _, question := range unresolved {
		statuses[question.MessageID] = question.Status
	}
	if !reflect.DeepEqual(statuses, map[string]string{required.ID: store.QuestionStatusOpen, reopened.ID: store.QuestionStatusSubmitted}) {
		t.Fatalf("unexpected unresolved questions: %#v", unresolved)
	}
}

func QuestionExpiryAndAccess(t *testing.T, st store.Store, hooks Hooks) {
	t.Helper()
	ctx := context.Background()
	f := newFixture(t, st)
	outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Outsider", Email: "question-outsider@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	invalid := questionSpec(outsider.ID)
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.bot.ID, Body: "who?", Question: &invalid}); !errors.Is(err, store.ErrInvalidQuestion) {
		t.Fatalf("responder outside the workspace error = %v", err)
	}
	guest, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Guest", Email: "question-guest@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, f.workspace.ID, guest.ID, store.WorkspaceRoleGuest); err != nil {
		t.Fatal(err)
	}
	guestResponder := questionSpec(guest.ID)
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.bot.ID, Body: "guest?", Question: &guestResponder}); !errors.Is(err, store.ErrInvalidQuestion) {
		t.Fatalf("guest responder outside the guest channel error = %v", err)
	}

	spec := questionSpec()
	expiring, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.bot.ID, Body: "expires", Question: &spec})
	if err != nil {
		t.Fatal(err)
	}
	hooks.ExpireQuestion(t, expiring.ID)
	fetched, err := st.GetMessage(ctx, expiring.ID, f.member.ID)
	if err != nil || fetched.Question.Status != store.QuestionStatusExpired {
		t.Fatalf("expired question reads as %#v, %v", fetched.Question, err)
	}
	if _, _, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: expiring.ID, UserID: f.member.ID, Skip: true}); !errors.Is(err, store.ErrQuestionClosed) {
		t.Fatalf("answering an expired question error = %v", err)
	}
	if recorded, _, err := st.ResolveQuestion(ctx, store.ResolveQuestionInput{MessageID: expiring.ID, BotUserID: f.bot.ID, Status: store.QuestionStatusExpired}); err != nil || recorded.Question.Status != store.QuestionStatusExpired || recorded.Question.ResolvedAt == nil {
		t.Fatalf("recording expiry = %#v, %v", recorded.Question, err)
	}

	conversation, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: f.workspace.ID, UserID: f.bot.ID, MemberIDs: []string{f.member.ID}})
	if err != nil {
		t.Fatal(err)
	}
	notInDirect := questionSpec(f.owner.ID)
	if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: conversation.ID, AuthorID: f.bot.ID, Body: "private?", Question: &notInDirect}); !errors.Is(err, store.ErrInvalidQuestion) {
		t.Fatalf("responder outside the direct conversation error = %v", err)
	}
	directSpec := questionSpec()
	direct, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: conversation.ID, AuthorID: f.bot.ID, Body: "private question", Question: &directSpec})
	if err != nil || direct.Question == nil {
		t.Fatalf("direct question = %#v, %v", direct, err)
	}
	if _, _, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: direct.ID, UserID: f.owner.ID, Skip: true}); err == nil {
		t.Fatal("people outside a direct conversation must not answer its questions")
	}
	if answered, events, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: direct.ID, UserID: f.member.ID, Skip: true}); err != nil || len(events) != 2 || len(events[1].RecipientUserIDs) != 2 {
		t.Fatalf("direct answer = %#v, %#v, %v", answered.Question, events, err)
	}

	root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.member.ID, Body: "@bot ship it"})
	if err != nil {
		t.Fatal(err)
	}
	threadSpec := questionSpec()
	reply, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: root.ID, AuthorID: f.bot.ID, Body: "Which day?", Question: &threadSpec})
	if err != nil || reply.Question == nil {
		t.Fatalf("thread question = %#v, %v", reply, err)
	}
	threadPage, err := st.GetThreadPage(ctx, root.ID, f.member.ID, store.ThreadPageRequest{MessagePageRequest: store.MessagePageRequest{Limit: 20}})
	if err != nil || len(threadPage.Replies) != 1 || threadPage.Replies[0].Question == nil {
		t.Fatalf("thread page did not hydrate the question: %#v, %v", threadPage.Replies, err)
	}
	if _, _, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: reply.ID, UserID: f.member.ID, Answers: map[string][]string{"ship_date": {"Lun 15 sep"}, "boxes": {"8"}}}); err != nil {
		t.Fatalf("thread answer error = %v", err)
	}

	gone, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.bot.ID, Body: "deleted soon", Question: &threadSpec})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: gone.ID, UserID: f.bot.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: gone.ID, UserID: f.member.ID, Skip: true}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("answering a deleted message error = %v", err)
	}
}

// QuestionConcurrentAnswers checks that exactly one of several racing answers wins.
func QuestionConcurrentAnswers(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	f := newFixture(t, st)
	spec := questionSpec()
	message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.bot.ID, Body: "race", Question: &spec})
	if err != nil {
		t.Fatal(err)
	}
	const racers = 8
	results := make(chan error, racers)
	start := make(chan struct{})
	for index := range racers {
		userID := f.member.ID
		if index%2 == 0 {
			userID = f.owner.ID
		}
		go func() {
			<-start
			_, _, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{
				MessageID: message.ID,
				UserID:    userID,
				Answers:   map[string][]string{"ship_date": {"Mar 16 sep"}, "boxes": {"1"}},
				Nonce:     "race-" + string(rune('a'+index)),
			})
			results <- err
		}()
	}
	close(start)
	wins := 0
	for range racers {
		err := <-results
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, store.ErrQuestionClosed):
			t.Fatalf("unexpected racing answer error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("expected exactly one winning answer, got %d", wins)
	}
	final, err := st.GetMessage(ctx, message.ID, f.owner.ID)
	if err != nil || final.Question.Status != store.QuestionStatusSubmitted || final.Question.Version != 2 {
		t.Fatalf("final question = %#v, %v", final.Question, err)
	}
}

func questionSpec(responders ...string) store.QuestionSpec {
	return store.QuestionSpec{
		ExternalID:       "ask_1",
		ExpiresAt:        time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339),
		ResponderUserIDs: responders,
		Items: []store.QuestionItem{
			{ID: "ship_date", Header: "Fecha", Prompt: "¿Qué día embarcamos?", Options: []store.QuestionOption{{Label: "Lun 15 sep"}, {Label: "Mar 16 sep"}}, AllowOther: true},
			{ID: "boxes", Header: "Cajas", Prompt: "¿Cuántas cajas?"},
		},
	}
}

func messageByID(messages []store.Message, id string) *store.Message {
	for index := range messages {
		if messages[index].ID == id {
			return &messages[index]
		}
	}
	return nil
}

func newFixture(t *testing.T, st store.Store) fixture {
	t.Helper()
	ctx := context.Background()
	var f fixture
	var err error
	if f.owner, err = st.EnsureBootstrap(ctx, "Owner", "question-owner@example.com"); err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, f.owner.ID)
	if err != nil || len(workspaces) != 1 {
		t.Fatalf("bootstrap workspaces = %#v, %v", workspaces, err)
	}
	f.workspace = workspaces[0]
	channels, err := st.ListChannels(ctx, f.workspace.ID, f.owner.ID)
	if err != nil || len(channels) == 0 {
		t.Fatalf("bootstrap channels = %#v, %v", channels, err)
	}
	f.channel = channels[0]
	if f.member, err = st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "question-member@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, f.workspace.ID, f.member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Question Bot", "Other Bot"} {
		bot, _, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: f.workspace.ID, DisplayName: name, Scopes: []string{"bot:write"}, CreatedBy: f.owner.ID})
		if err != nil {
			t.Fatal(err)
		}
		if name == "Question Bot" {
			f.bot = bot
		} else {
			f.otherBot = bot
		}
	}
	return f
}

// QuestionReplayAndVersionGuards checks retries around deadlines and reopens,
// activity rows, and direct-message visibility in reconciliation.
func QuestionReplayAndVersionGuards(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	f := newFixture(t, st)

	// An identical retry after the deadline window shrinks still finds the original.
	nearDeadline := questionSpec()
	nearDeadline.ExpiresAt = time.Now().Add(store.MinQuestionLifetime + 1500*time.Millisecond).UTC().Format(time.RFC3339Nano)
	original, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.bot.ID, Body: "retry me", Nonce: "question-near-deadline", Question: &nearDeadline})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	replayed, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.bot.ID, Body: "retry me", Nonce: "question-near-deadline", Question: &nearDeadline})
	if err != nil || replayed.ID != original.ID {
		t.Fatalf("replay near the deadline = %s, %v; want %s", replayed.ID, err, original.ID)
	}
	fresh := nearDeadline
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.bot.ID, Body: "too late", Nonce: "question-too-late", Question: &fresh}); !errors.Is(err, store.ErrInvalidQuestion) {
		t.Fatalf("new question inside the minimum lifetime error = %v", err)
	}

	activitySpec := questionSpec()
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.bot.ID, Body: "running a tool", Kind: store.MessageKindAgentTool, TurnID: "turn_question", Question: &activitySpec}); !errors.Is(err, store.ErrInvalidQuestion) {
		t.Fatalf("question on an activity row error = %v", err)
	}

	spec := questionSpec()
	message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.bot.ID, Body: "versioned", Question: &spec})
	if err != nil {
		t.Fatal(err)
	}
	answers := map[string][]string{"ship_date": {"Otra fecha"}, "boxes": {"12"}}
	stale := int64(7)
	if _, _, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: message.ID, UserID: f.member.ID, Answers: answers, Nonce: "versioned-answer", ExpectedVersion: &stale}); !errors.Is(err, store.ErrQuestionConflict) {
		t.Fatalf("answer to a version the person did not see error = %v", err)
	}
	seen := int64(1)
	if _, _, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: message.ID, UserID: f.member.ID, Answers: answers, Nonce: "versioned-answer", ExpectedVersion: &seen}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ResolveQuestion(ctx, store.ResolveQuestionInput{MessageID: message.ID, BotUserID: f.bot.ID, Status: store.QuestionStatusOpen, Note: "Pick a listed date"}); err != nil {
		t.Fatal(err)
	}
	// The first response was lost and the bot reopened; the retry must not resubmit it.
	if _, _, err := st.AnswerQuestion(ctx, store.AnswerQuestionInput{MessageID: message.ID, UserID: f.member.ID, Answers: answers, Nonce: "versioned-answer", ExpectedVersion: &seen}); !errors.Is(err, store.ErrQuestionConflict) {
		t.Fatalf("retry after reopen error = %v", err)
	}
	if current, err := st.GetMessage(ctx, message.ID, f.member.ID); err != nil || current.Question.Status != store.QuestionStatusOpen || current.Question.Response != nil {
		t.Fatalf("reopened question after a stale retry = %#v, %v", current.Question, err)
	}

	conversation, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: f.workspace.ID, UserID: f.bot.ID, MemberIDs: []string{f.member.ID}})
	if err != nil {
		t.Fatal(err)
	}
	directSpec := questionSpec()
	direct, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: conversation.ID, AuthorID: f.bot.ID, Body: "private", Question: &directSpec})
	if err != nil {
		t.Fatal(err)
	}
	withDirect, err := st.ListBotUnresolvedQuestions(ctx, f.workspace.ID, f.bot.ID, "", true, 50)
	if err != nil {
		t.Fatal(err)
	}
	withoutDirect, err := st.ListBotUnresolvedQuestions(ctx, f.workspace.ID, f.bot.ID, "", false, 50)
	if err != nil {
		t.Fatal(err)
	}
	if messageInQuestions(withDirect, direct.ID) == nil || messageInQuestions(withoutDirect, direct.ID) != nil || messageInQuestions(withoutDirect, message.ID) == nil {
		t.Fatalf("direct questions with scope = %#v, without = %#v", withDirect, withoutDirect)
	}
}

func messageInQuestions(questions []store.BotQuestion, messageID string) *store.BotQuestion {
	for index := range questions {
		if questions[index].MessageID == messageID {
			return &questions[index]
		}
	}
	return nil
}
