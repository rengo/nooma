package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/internal/store/vaultlock"
)

func init() {
	commands["reindex"] = command{
		summary: "embed the live units lacking the embedding model's vector, and rebuild the word index",
		run:     runReindex,
	}
}

// reindexProgressEvery is how many units pass between progress lines.
const reindexProgressEvery = 25

// runReindex is `nooma reindex [--yes] [vault]` (ADR-0003, ADR-0058).
func runReindex(args []string, out, errOut io.Writer) error {
	return reindex(context.Background(), args, os.Stdin, out, errOut)
}

// reindex embeds the live units lacking the configured model's vector and
// rebuilds the word index (design.md). It takes the vault lock as consolidate
// does, and asks before embedding unless --yes.
func reindex(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("reindex", flag.ContinueOnError)
	fs.SetOutput(errOut)
	yes := fs.Bool("yes", false, "embed without asking first")
	fs.Usage = func() { _, _ = fmt.Fprint(errOut, "usage: nooma reindex [--yes] [vault]\n") }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("reindex takes at most one vault path, got %d", fs.NArg())
	}
	vault, err := config.ResolveVault(fs.Arg(0))
	if err != nil {
		return err
	}
	cfg, err := loadVaultConfig(vault)
	if err != nil {
		return err
	}
	dbPath, err := cfg.DatabasePath(vault)
	if err != nil {
		return err
	}
	model := configuredEmbeddingModel(cfg)
	var embed ports.EmbeddingProvider
	var providerName string
	if model != "" { // resolved before the lock, as consolidate does
		if embed, providerName, err = reindexProvider(cfg); err != nil {
			return err
		}
	}

	lock, err := vaultlock.Acquire(vault)
	if err != nil {
		var inUse *vaultlock.InUseError
		if errors.As(err, &inUse) {
			return fmt.Errorf("%w: stop nooma serve, run nooma reindex, and start it again", err)
		}
		return fmt.Errorf("taking the vault lock: %w", err)
	}
	defer func() { _ = lock.Release() }()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	say := func(format string, a ...any) { _, _ = fmt.Fprintf(out, "reindex: "+format+"\n", a...) }
	embeds := sqlite.NewEmbeddingRepo(db)
	missing, had := 0, 0
	if model != "" {
		if missing, err = embeds.CountLiveWithoutEmbedding(ctx, model); err != nil {
			return err
		}
		if had, err = liveWithModel(ctx, embeds, model); err != nil {
			return err
		}
	}
	if missing > 0 {
		say("%s %s a %s vector; each is one call to provider %q", countNoun(missing, "live unit", "live units"), lacks(missing), model, providerName)
		if !*yes {
			_, _ = fmt.Fprint(out, "Continue? [y/N] ")
			if answer := strings.ToLower(readLine(bufio.NewReader(in))); answer != "y" && answer != "yes" {
				_, _ = fmt.Fprintln(out)
				say("nothing was changed; run it again and answer y, or pass --yes to embed without asking")
				return nil
			}
		}
	}
	if err := db.RebuildSearchIndex(ctx); err != nil {
		return fmt.Errorf("rebuilding the word index: %w", err)
	}
	say("word index rebuilt")
	switch {
	case model == "":
		say("no embedding model is configured (tasks.embedding in nooma.yml), so no vectors were computed")
		return nil
	case missing == 0:
		say("nothing to embed: every live unit already has a %s vector", model)
		return nil
	}

	// Bound after the question: Ctrl-C there ends a command that wrote nothing.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	svc := brain.NewReindexService(systemClock{}, sqlite.NewUnitRepo(db), embeds, embed)
	res, runErr := svc.Reindex(ctx, model, func(done, total int) {
		if done%reindexProgressEvery == 0 || done == total {
			say("%d of %d done under %s", done, total, model)
		}
	})
	say("embedded %d, skipped %d already embedded, failed %d", res.Embedded, had, res.Failed)
	if res.LastFailure != nil {
		say("last failure: %v", res.LastFailure)
	}
	// Not through ctx, which an interrupted run has cancelled.
	left, err := embeds.CountLiveWithoutEmbedding(context.Background(), model)
	if err != nil {
		return err
	}
	stillLeft := fmt.Sprintf("%s still %s a %s vector; run nooma reindex again to continue, nothing stored is redone", countNoun(left, "live unit", "live units"), lacks(left), model)
	if left == 0 {
		say("done: every live unit has a %s vector", model)
	} else {
		say("%s", stillLeft)
	}
	switch {
	case errors.Is(runErr, context.Canceled):
		return errors.New("reindex interrupted; what was stored is kept, run nooma reindex again to continue")
	case runErr != nil:
		return fmt.Errorf("reindex stopped: %w; what was stored is kept, run nooma reindex again to continue", runErr)
	case res.Failed > 0:
		return fmt.Errorf("%s could not be embedded; run nooma reindex again to retry", countNoun(res.Failed, "unit", "units"))
	case left > 0: // never on a correct run; a run that stops short is not silent
		return errors.New(stillLeft)
	}
	return nil
}

// reindexProvider builds the provider tasks.embedding is bound to, and names it.
func reindexProvider(cfg *config.Config) (ports.EmbeddingProvider, string, error) {
	name := cfg.Tasks["embedding"].Provider
	client, err := buildProvider(cfg.Providers[name], os.LookupEnv)
	if err != nil {
		return nil, "", fmt.Errorf("reindex: tasks.embedding: %w", err)
	}
	embed, ok := client.(ports.EmbeddingProvider)
	if !ok {
		return nil, "", fmt.Errorf("reindex: tasks.embedding is bound to provider %q, which cannot embed", name)
	}
	return embed, name, nil
}

// liveWithModel is how many live units already hold a vector under model.
func liveWithModel(ctx context.Context, embeds *sqlite.EmbeddingRepo, model string) (int, error) {
	models, err := embeds.LiveModels(ctx)
	if err != nil {
		return 0, err
	}
	for _, mc := range models {
		if mc.Model == model {
			return mc.Units, nil
		}
	}
	return 0, nil
}

// countNoun is n with the noun in its number.
func countNoun(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// lacks agrees with n.
func lacks(n int) string {
	if n == 1 {
		return "lacks"
	}
	return "lack"
}
