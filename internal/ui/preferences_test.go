package ui_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ui"
)

// fakePreferences is ui.Preferences over fixed values: Save records what it
// was given and answers result and err.
type fakePreferences struct {
	current brain.Preferences
	result  brain.SaveResult
	err     error
	saved   []brain.Preferences
}

func (f *fakePreferences) Preferences(context.Context) (brain.Preferences, error) {
	return f.current, nil
}

func (f *fakePreferences) Save(_ context.Context, p brain.Preferences) (brain.SaveResult, error) {
	f.saved = append(f.saved, p)
	return f.result, f.err
}

func defaultPreferences() brain.Preferences {
	return brain.Preferences{LeadMinutes: []int{1440, 120}, DateOnlyAt: "09:00", QuietEnd: "07:00", EveningAt: "22:00"}
}

func preferencesGet(t *testing.T, prefs ui.Preferences) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/ui/preferences", nil)
	req.Pattern = "GET /ui/preferences"
	rec := httptest.NewRecorder()
	ui.New(ui.Deps{Preferences: prefs}).ServeHTTP(rec, req)
	return rec
}

func preferencesPost(t *testing.T, prefs ui.Preferences, body string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/ui/preferences", strings.NewReader(body))
	req.Pattern = "POST /ui/preferences"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	ui.New(ui.Deps{Preferences: prefs}).ServeHTTP(rec, req)
	return rec
}

const validForm = "lead_amount=3&lead_unit=hours&lead_amount=&lead_unit=hours&date_only_reminder_at=08:30&quiet_hours_end=07:00&evening_reminder_at=22:00"

func assertContains(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
}

func TestPreferencesPage_ShowsEveryValueInForce(t *testing.T) {
	t.Parallel()
	rec := preferencesGet(t, &fakePreferences{current: defaultPreferences()})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	assertContains(t, body,
		`<a href="/ui/preferences" aria-current="page">Preferences</a>`,
		`name="lead_amount" value="24"`, `name="lead_amount" value="2"`,
		`<option value="hours" selected>hours</option>`,
		`name="date_only_reminder_at" value="09:00"`, `name="quiet_hours_end" value="07:00"`, `name="evening_reminder_at" value="22:00"`,
		"also moves the reminders already set for upcoming events",
	)
	// Two leads plus one empty row to add a third.
	if n := strings.Count(body, `name="lead_amount"`); n != 3 {
		t.Errorf("%d lead rows, want the two leads and one empty row", n)
	}
	assertPendingForm(t, between(t, body, `<form method="post" action="/ui/preferences"`, "</form>"), "Save preferences", "Saving…")
}

func TestPreferencesPage_NotWiredIs503(t *testing.T) {
	t.Parallel()
	if rec := preferencesGet(t, nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestPreferencesSave_SendsTheFormAndShowsTheSavedValues(t *testing.T) {
	t.Parallel()
	saved := brain.Preferences{LeadMinutes: []int{180}, DateOnlyAt: "08:30", QuietEnd: "07:00", EveningAt: "22:00"}
	fake := &fakePreferences{result: brain.SaveResult{Changed: []brain.PreferenceField{brain.FieldLeads, brain.FieldDateOnlyAt}, Saved: saved}}
	rec := preferencesPost(t, fake, validForm)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200:\n%s", rec.Code, rec.Body.String())
	}
	if len(fake.saved) != 1 || !slices.Equal(fake.saved[0].LeadMinutes, []int{180}) || fake.saved[0].DateOnlyAt != "08:30" ||
		fake.saved[0].QuietEnd != "07:00" || fake.saved[0].EveningAt != "22:00" {
		t.Fatalf("Save got %+v, want leads [180] (the empty row ignored) and the three times", fake.saved)
	}
	assertContains(t, rec.Body.String(), `data-outcome="saved"`, "Preferences saved.", `name="lead_amount" value="3"`, `name="date_only_reminder_at" value="08:30"`)
}

func TestPreferencesSave_UnitsConvertToMinutes(t *testing.T) {
	t.Parallel()
	fake := &fakePreferences{}
	preferencesPost(t, fake, "lead_amount=2&lead_unit=days&lead_amount=45&lead_unit=minutes&date_only_reminder_at=09:00&quiet_hours_end=07:00&evening_reminder_at=22:00")
	if len(fake.saved) != 1 || !slices.Equal(fake.saved[0].LeadMinutes, []int{2880, 45}) {
		t.Fatalf("Save got %+v, want [2880 45]", fake.saved)
	}
}

func TestPreferencesSave_NothingChangedSaysSo(t *testing.T) {
	t.Parallel()
	fake := &fakePreferences{result: brain.SaveResult{Saved: defaultPreferences()}}
	assertContains(t, preferencesPost(t, fake, validForm).Body.String(), `data-outcome="saved"`, "Nothing changed.")
}

func TestPreferencesSave_RefusedFieldsAreNamedAndTheInputKept(t *testing.T) {
	t.Parallel()
	fake := &fakePreferences{err: &brain.PreferencesError{Fields: map[brain.PreferenceField]error{
		brain.FieldLeads:     prospection.ErrInvalidReminderPref,
		brain.FieldEveningAt: prospection.ErrInvalidReminderPref,
	}}}
	rec := preferencesPost(t, fake, "lead_amount=2&lead_unit=hours&lead_amount=120&lead_unit=minutes&date_only_reminder_at=09:00&quiet_hours_end=07:00&evening_reminder_at=06:00")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	body := rec.Body.String()
	assertContains(t, body, `data-outcome="invalid"`, "Nothing was saved.",
		`data-error="event_reminder_leads"`, "no two the same",
		`data-error="evening_reminder_at"`, "at or after the end of quiet hours",
		`name="lead_amount" value="120"`, `name="evening_reminder_at" value="06:00"`)
	if strings.Contains(body, `data-error="quiet_hours_end"`) || strings.Contains(body, `data-error="date_only_reminder_at"`) {
		t.Error("a field that was not refused shows an error")
	}
}

func TestPreferencesSave_AnAmountThatIsNotAWholeNumberIsRefusedBeforeTheBrain(t *testing.T) {
	t.Parallel()
	for _, amount := range []string{"abc", "1.5", "-2"} {
		fake := &fakePreferences{}
		rec := preferencesPost(t, fake, "lead_amount="+amount+"&lead_unit=hours&date_only_reminder_at=09:00&quiet_hours_end=07:00&evening_reminder_at=22:00")
		if amount == "-2" {
			// A whole number, so it reaches the brain, which refuses it.
			if len(fake.saved) != 1 || fake.saved[0].LeadMinutes[0] != -120 {
				t.Errorf("amount %q: Save got %+v, want -120 passed on for the brain to refuse", amount, fake.saved)
			}
			continue
		}
		if rec.Code != http.StatusBadRequest || len(fake.saved) != 0 {
			t.Errorf("amount %q: status %d with %d saves, want 400 and none", amount, rec.Code, len(fake.saved))
		}
		assertContains(t, rec.Body.String(), `data-error="event_reminder_leads"`)
	}
}

func TestPreferencesSave_AHugeAmountIsStillOutOfRange(t *testing.T) {
	t.Parallel()
	fake := &fakePreferences{}
	preferencesPost(t, fake, "lead_amount=7000000000000000&lead_unit=days&date_only_reminder_at=09:00&quiet_hours_end=07:00&evening_reminder_at=22:00")
	if len(fake.saved) != 1 || fake.saved[0].LeadMinutes[0] <= prospection.MaxEventLeadDays*24*60 {
		t.Errorf("Save got %+v, want a lead past the bound rather than an overflowed one", fake.saved)
	}
}

func TestPreferencesSave_WrittenButNotFullyFollowedIsANotice(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"record":    {&brain.WriteLandedError{Record: true, Err: errors.New("x")}, "the activity record of it could not be written"},
		"reminders": {brain.ErrRemindersNotFollowed, "Save again to finish"},
	} {
		fake := &fakePreferences{result: brain.SaveResult{Saved: defaultPreferences()}, err: tc.err}
		rec := preferencesPost(t, fake, validForm)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", name, rec.Code)
		}
		assertContains(t, rec.Body.String(), `data-outcome="saved-with-notice"`, tc.want)
	}
}

func TestPreferencesSave_AddAndRemoveEditTheRowsWithoutSaving(t *testing.T) {
	t.Parallel()
	fake := &fakePreferences{}
	rec := preferencesPost(t, fake, "op=add&"+validForm, "HX-Request", "true")
	if n := strings.Count(rec.Body.String(), `name="lead_amount"`); n != 3 || len(fake.saved) != 0 {
		t.Errorf("add: %d rows and %d saves, want the two rows plus one and no save:\n%s", n, len(fake.saved), rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "<nav") {
		t.Error("add answered a full page to an htmx request, want the leads fragment")
	}

	rec = preferencesPost(t, fake, "op=remove-0&"+validForm, "HX-Request", "true")
	body := rec.Body.String()
	if strings.Contains(body, `value="3"`) || !strings.Contains(body, `name="lead_amount"`) || len(fake.saved) != 0 {
		t.Errorf("remove-0: the first row is still there, or a save ran:\n%s", body)
	}
}

func TestPreferencesSave_AtMostFiveRowsAndAddIsGoneAtFive(t *testing.T) {
	t.Parallel()
	five := "lead_amount=1&lead_unit=hours&lead_amount=2&lead_unit=hours&lead_amount=3&lead_unit=hours&lead_amount=4&lead_unit=hours&lead_amount=5&lead_unit=hours&date_only_reminder_at=09:00&quiet_hours_end=07:00&evening_reminder_at=22:00"
	body := preferencesPost(t, &fakePreferences{}, "op=add&"+five, "HX-Request", "true").Body.String()
	if n := strings.Count(body, `name="lead_amount"`); n != prospection.MaxEventReminderLeads {
		t.Errorf("%d rows, want at most %d", n, prospection.MaxEventReminderLeads)
	}
	if strings.Contains(body, `value="add"`) {
		t.Error("the add button is offered with every row taken")
	}
}

func TestPreferencesSave_BoostedAnswerKeepsThePageURL(t *testing.T) {
	t.Parallel()
	fake := &fakePreferences{result: brain.SaveResult{Saved: defaultPreferences()}}
	rec := preferencesPost(t, fake, validForm, "HX-Request", "true", "HX-Boosted", "true")
	if got := rec.Header().Get("HX-Replace-Url"); got != "/ui/preferences" {
		t.Errorf("HX-Replace-Url = %q, want /ui/preferences", got)
	}
	assertContains(t, rec.Body.String(), "<nav")
}

// A sixth lead typed past the editor reaches the brain, whose refusal names
// the leads, rather than being dropped in silence.
func TestPreferencesSave_ExtraRowsAreNotDropped(t *testing.T) {
	t.Parallel()
	fake := &fakePreferences{}
	body := "date_only_reminder_at=09:00&quiet_hours_end=07:00&evening_reminder_at=22:00"
	for i := 1; i <= 6; i++ {
		body += "&lead_amount=" + strconv.Itoa(i) + "&lead_unit=hours"
	}
	preferencesPost(t, fake, body)
	if len(fake.saved) != 1 || len(fake.saved[0].LeadMinutes) != 6 {
		t.Errorf("Save got %+v, want all six leads for the brain to judge", fake.saved)
	}
}
