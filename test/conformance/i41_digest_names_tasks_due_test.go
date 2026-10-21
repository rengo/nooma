// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// task stores an open task due at due, titled title, that nobody asked to be
// reminded of: it arms nothing (doc 02 §7).
func (v *i37Vault) task(id, title string, due time.Time) {
	v.t.Helper()
	if err := v.units.Create(context.Background(), unit.Unit{
		ID: id, Type: unit.TypeTask, Status: unit.StatusPool, Content: title + ".", Title: title, Weight: 1,
		DueAt: &due, LastTouchedAt: i37Now, CreatedAt: i37Now.AddDate(0, 0, -1), UpdatedAt: i37Now,
	}); err != nil {
		v.t.Fatalf("seed task %s: %v", id, err)
	}
}

// TestI41_TheDigestNamesATaskOnItsDueDay: "mañana renuevo el pasaporte" is a
// task due tomorrow, and a task arms no reminder. Nothing rings the day it is
// told; the next morning's digest names it, by its title and its day, and a
// digest that has nothing else to say is still sent for it.
func TestI41_TheDigestNamesATaskOnItsDueDay(t *testing.T) {
	v := newI37Vault(t)
	v.task("passport", "Renew the passport", time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))

	if v.check(false); len(v.ch.texts) != 0 {
		t.Fatalf("the day it was told, sent %q; want nothing", v.ch.texts)
	}

	report := v.checkAt(time.Date(2026, 10, 15, 7, 0, 0, 0, time.UTC), false)
	want := "Here is 1 thing for today:\n• Renew the passport — Thu 15 Oct"
	if len(v.ch.texts) != 1 || v.ch.texts[0] != want {
		t.Fatalf("on its due day, sent %q; want exactly [%q]", v.ch.texts, want)
	}
	// The carried count is the day's reminders, and a task is none.
	if report.DigestCarried != 0 || len(v.rows(ports.ActionCheckDigestSent)) != 1 {
		t.Errorf("report %+v, digest rows %d; want no reminder carried and one digest recorded",
			report, len(v.rows(ports.ActionCheckDigestSent)))
	}
}

// TestI41_AnOverdueTaskIsNamedForDueTaskDigestDaysMornings: a task still open
// after its day is named on the mornings after it, DueTaskDigestDays mornings
// in all, and then no more: the bound stops the line of a task nobody answers
// (a done in reply closes it, I42).
func TestI41_AnOverdueTaskIsNamedForDueTaskDigestDaysMornings(t *testing.T) {
	v := newI37Vault(t)
	v.task("passport", "Renew the passport", time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))

	for day := 15; day < 15+prospection.DueTaskDigestDays; day++ {
		v.ch.texts = nil
		v.checkAt(time.Date(2026, 10, day, 7, 0, 0, 0, time.UTC), false)
		want := "Here is 1 thing for today:\n• Renew the passport — Thu 15 Oct"
		if len(v.ch.texts) != 1 || v.ch.texts[0] != want {
			t.Errorf("Oct %d: sent %q; want exactly [%q]", day, v.ch.texts, want)
		}
	}
	v.ch.texts = nil
	v.checkAt(time.Date(2026, 10, 15+prospection.DueTaskDigestDays, 7, 0, 0, 0, time.UTC), false)
	if len(v.ch.texts) != 0 {
		t.Errorf("past the bound, sent %q; want nothing", v.ch.texts)
	}
}

// TestI41_ADueTaskIsNamedOnceADay: the digest is once a day, and a task it
// named is not named again by a later pass that morning.
func TestI41_ADueTaskIsNamedOnceADay(t *testing.T) {
	v := newI37Vault(t)
	v.task("passport", "Renew the passport", time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))

	v.checkAt(time.Date(2026, 10, 15, 7, 0, 0, 0, time.UTC), false)
	v.checkAt(time.Date(2026, 10, 15, 7, 5, 0, 0, time.UTC), false)
	if len(v.ch.texts) != 1 {
		t.Errorf("sent %q; want one digest", v.ch.texts)
	}
}

// TestI41_ADryRunNamesNothing: a dry run sends nothing and records no digest.
func TestI41_ADryRunNamesNothing(t *testing.T) {
	v := newI37Vault(t)
	v.task("passport", "Renew the passport", time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))

	v.checkAt(time.Date(2026, 10, 15, 7, 0, 0, 0, time.UTC), true)
	if len(v.ch.texts) != 0 || len(v.rows(ports.ActionCheckDigestSent)) != 0 {
		t.Errorf("a dry run sent %q and recorded %d digest(s); want nothing", v.ch.texts, len(v.rows(ports.ActionCheckDigestSent)))
	}
}

// TestI41_ADatedTaskIsAnsweredWithItsDayAndTheDigest: told on Friday, a task
// due Saturday arms nothing and its reply names the day and the digest that
// will mention it, and its hour when it was told one; a task due today, or
// with no date, is "Anotado.", since no later morning is promised.
func TestI41_ADatedTaskIsAnsweredWithItsDayAndTheDigest(t *testing.T) {
	for _, c := range []struct{ name, reading, want string }{
		{"due tomorrow", i36Reading("task", "2026-10-17", false, "es"),
			"Anotado para el sáb 17 oct. Te lo recuerdo en el resumen de esa mañana."},
		{"due tomorrow at an hour", i36Reading("task", "2026-10-17T10:00:00-03:00", false, "es"),
			"Anotado para el sáb 17 oct, 10:00. Te lo recuerdo en el resumen de esa mañana."},
		{"due tomorrow at midnight", i36Reading("task", "2026-10-17T00:00:00-03:00", false, "es"),
			"Anotado para el sáb 17 oct, 00:00. Te lo recuerdo en el resumen de esa mañana."},
		{"due today", i36Reading("task", "2026-10-16", false, "es"), "Anotado."},
		{"no date", `{"type":"task","normalized_content":"Comprar café","weight":0.7,"decay_rate":0.03,"language":"es"}`, "Anotado."},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := newI36Vault()
			result := v.say(t, i36Now, c.reading)
			if v.triggers.Count() != 0 || result.Outcome != brain.OutcomeStored {
				t.Fatalf("result = %+v, triggers %d; want the task stored and nothing armed", result, v.triggers.Count())
			}
			if got := channels.RenderReply(result); got != c.want {
				t.Errorf("reply = %q, want %q", got, c.want)
			}
		})
	}
}
