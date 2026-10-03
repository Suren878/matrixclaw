package clientcmd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/daemonclient"
	appsetup "github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/skills"
)

func runSkillsCommand(stdout io.Writer, stderr io.Writer, binaryName string, service *appsetup.Service, args []string) int {
	subcommand := ""
	if len(args) > 0 {
		subcommand = strings.TrimSpace(args[0])
	}
	switch subcommand {
	case "", "list":
		return withSkillsDaemon(stderr, binaryName, service, "skills list", func(ctx context.Context, client *daemonclient.Client) error {
			items, err := client.ListSkills(ctx, skills.SearchOptions{IncludeQuarantined: true, IncludeArchived: true, IncludeDisabled: true, Limit: 200})
			if err != nil {
				return err
			}
			printSkills(stdout, binaryName, items)
			return nil
		})
	case "search":
		query := strings.Join(args[1:], " ")
		return withSkillsDaemon(stderr, binaryName, service, "skills search", func(ctx context.Context, client *daemonclient.Client) error {
			items, err := client.SearchSkills(ctx, query, skills.SearchOptions{Limit: 50})
			if err != nil {
				return err
			}
			printSkills(stdout, binaryName, items)
			return nil
		})
	case "show":
		if len(args) < 2 {
			_, _ = fmt.Fprintf(stderr, "%s: skills show: ID is required\n", binaryName)
			return 2
		}
		return withSkillsDaemon(stderr, binaryName, service, "skills show", func(ctx context.Context, client *daemonclient.Client) error {
			detail, err := client.GetSkill(ctx, args[1])
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(stdout, "%s: skill %s [%s/%s] %s\n\n%s\n", binaryName, detail.Skill.ID, detail.Skill.TrustState, detail.Skill.State, detail.Skill.Description, detail.Body)
			return nil
		})
	case "install":
		if len(args) < 2 {
			_, _ = fmt.Fprintf(stderr, "%s: skills install: PATH is required\n", binaryName)
			return 2
		}
		return withSkillsDaemon(stderr, binaryName, service, "skills install", func(ctx context.Context, client *daemonclient.Client) error {
			path, err := skillInstallSource(args[1])
			if err != nil {
				return err
			}
			items, err := client.InstallSkill(ctx, path)
			if err != nil {
				return err
			}
			printSkills(stdout, binaryName, items)
			return nil
		})
	case "trust", "quarantine", "enable", "disable", "remove", "archive", "restore", "pin", "unpin":
		if len(args) < 2 {
			_, _ = fmt.Fprintf(stderr, "%s: skills %s: ID is required\n", binaryName, subcommand)
			return 2
		}
		return withSkillsDaemon(stderr, binaryName, service, "skills "+subcommand, func(ctx context.Context, client *daemonclient.Client) error {
			var err error
			if subcommand == "remove" {
				err = client.RemoveSkill(ctx, args[1])
			} else {
				err = client.SkillAction(ctx, args[1], subcommand)
			}
			if err == nil {
				_, _ = fmt.Fprintf(stdout, "%s: %s %s\n", binaryName, subcommand, args[1])
			}
			return err
		})
	case "usage":
		return withSkillsDaemon(stderr, binaryName, service, "skills usage", func(ctx context.Context, client *daemonclient.Client) error {
			items, err := client.SkillUsage(ctx)
			if err != nil {
				return err
			}
			printSkills(stdout, binaryName, items)
			return nil
		})
	case "help", "-h", "--help":
		printSkillsUsage(stdout, binaryName)
		return 0
	default:
		printSkillsUsage(stdout, binaryName)
		return 2
	}
}

// withSkillsDaemon runs fn against the daemon, which owns the skills library.
func withSkillsDaemon(stderr io.Writer, binaryName string, service *appsetup.Service, contextLabel string, fn func(context.Context, *daemonclient.Client) error) int {
	ctx := context.Background()
	if _, err := ensureDaemon(ctx, service); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %s: ensure daemon: %v\n", binaryName, contextLabel, err)
		return 1
	}
	cfg, err := service.Load()
	if err != nil {
		return handleSetupReadError(stderr, binaryName, service, contextLabel, err)
	}
	if err := fn(ctx, configuredDaemonClient(cfg)); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %s: %v\n", binaryName, contextLabel, err)
		return 1
	}
	return 0
}

func printSkills(w io.Writer, binaryName string, items []skills.Skill) {
	if len(items) == 0 {
		_, _ = fmt.Fprintf(w, "%s: skills: none\n", binaryName)
		return
	}
	for _, item := range items {
		status := "disabled"
		if item.Enabled {
			status = "enabled"
		}
		_, _ = fmt.Fprintf(w, "%s: skill %s [%s/%s/%s] %s\n", binaryName, item.ID, item.TrustState, status, item.State, item.Description)
	}
}

// skillInstallSource is a local path made absolute for the daemon, or an
// http(s) URL as given.
func skillInstallSource(arg string) (string, error) {
	if lower := strings.ToLower(arg); strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") {
		return arg, nil
	}
	return filepath.Abs(arg)
}

func printSkillsUsage(w io.Writer, binaryName string) {
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintf(w, "  %s skills list\n", binaryName)
	_, _ = fmt.Fprintf(w, "  %s skills search QUERY\n", binaryName)
	_, _ = fmt.Fprintf(w, "  %s skills show ID\n", binaryName)
	_, _ = fmt.Fprintf(w, "  %s skills install PATH\n", binaryName)
	_, _ = fmt.Fprintf(w, "  %s skills trust|quarantine|enable|disable|remove ID\n", binaryName)
	_, _ = fmt.Fprintf(w, "  %s skills archive|restore|pin|unpin ID\n", binaryName)
	_, _ = fmt.Fprintf(w, "  %s skills usage\n", binaryName)
}
