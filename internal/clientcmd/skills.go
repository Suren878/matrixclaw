package clientcmd

import (
	"fmt"
	"io"
	"strings"

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
		return withSkillsService(stdout, stderr, binaryName, service, "skills list", func(svc *skills.Service) int {
			items, err := svc.List(skills.SearchOptions{IncludeQuarantined: true, IncludeArchived: true, IncludeDisabled: true, Limit: 200})
			if err != nil {
				fmt.Fprintf(stderr, "%s: skills list: %v\n", binaryName, err)
				return 1
			}
			printSkills(stdout, binaryName, items)
			return 0
		})
	case "search":
		query := strings.Join(args[1:], " ")
		return withSkillsService(stdout, stderr, binaryName, service, "skills search", func(svc *skills.Service) int {
			items, err := svc.Search(query, skills.SearchOptions{Limit: 50})
			if err != nil {
				fmt.Fprintf(stderr, "%s: skills search: %v\n", binaryName, err)
				return 1
			}
			printSkills(stdout, binaryName, items)
			return 0
		})
	case "show":
		if len(args) < 2 {
			fmt.Fprintf(stderr, "%s: skills show: ID is required\n", binaryName)
			return 2
		}
		return withSkillsService(stdout, stderr, binaryName, service, "skills show", func(svc *skills.Service) int {
			detail, err := svc.Get(args[1])
			if err != nil {
				fmt.Fprintf(stderr, "%s: skills show: %v\n", binaryName, err)
				return 1
			}
			fmt.Fprintf(stdout, "%s: skill %s [%s/%s] %s\n\n%s\n", binaryName, detail.Skill.ID, detail.Skill.TrustState, detail.Skill.State, detail.Skill.Description, detail.Body)
			return 0
		})
	case "install":
		if len(args) < 2 {
			fmt.Fprintf(stderr, "%s: skills install: PATH is required\n", binaryName)
			return 2
		}
		return withSkillsService(stdout, stderr, binaryName, service, "skills install", func(svc *skills.Service) int {
			items, err := svc.InstallPath(args[1], skills.InstallOptions{Provenance: args[1]})
			if err != nil {
				fmt.Fprintf(stderr, "%s: skills install: %v\n", binaryName, err)
				return 1
			}
			printSkills(stdout, binaryName, items)
			return 0
		})
	case "trust", "quarantine", "enable", "disable", "remove", "archive", "restore", "pin", "unpin":
		if len(args) < 2 {
			fmt.Fprintf(stderr, "%s: skills %s: ID is required\n", binaryName, subcommand)
			return 2
		}
		return withSkillsService(stdout, stderr, binaryName, service, "skills "+subcommand, func(svc *skills.Service) int {
			if err := applySkillCLIAction(svc, subcommand, args[1]); err != nil {
				fmt.Fprintf(stderr, "%s: skills %s: %v\n", binaryName, subcommand, err)
				return 1
			}
			fmt.Fprintf(stdout, "%s: %s %s\n", binaryName, subcommand, args[1])
			return 0
		})
	case "usage":
		return withSkillsService(stdout, stderr, binaryName, service, "skills usage", func(svc *skills.Service) int {
			usage, err := svc.Usage()
			if err != nil {
				fmt.Fprintf(stderr, "%s: skills usage: %v\n", binaryName, err)
				return 1
			}
			printSkills(stdout, binaryName, usage.Skills)
			return 0
		})
	case "curator":
		return withSkillsService(stdout, stderr, binaryName, service, "skills curator", func(svc *skills.Service) int {
			result, err := svc.Curator()
			if err != nil {
				fmt.Fprintf(stderr, "%s: skills curator: %v\n", binaryName, err)
				return 1
			}
			printSkills(stdout, binaryName, result.Archived)
			return 0
		})
	case "help", "-h", "--help":
		printSkillsUsage(stdout, binaryName)
		return 0
	default:
		printSkillsUsage(stdout, binaryName)
		return 2
	}
}

func withSkillsService(stdout io.Writer, stderr io.Writer, binaryName string, service *appsetup.Service, contextLabel string, fn func(*skills.Service) int) int {
	cfg, err := service.Load()
	if err != nil {
		return handleSetupReadError(stderr, binaryName, service, contextLabel, err)
	}
	svc, err := skills.NewService(skills.Config{
		DBPath:      cfg.Daemon.DBPath,
		Enabled:     cfg.Modules.Skills.Enabled,
		AutoInvoke:  cfg.Modules.Skills.AutoInvoke,
		TrustPolicy: cfg.Modules.Skills.TrustPolicy,
		SelfImprove: cfg.Modules.Skills.SelfImprove,
	})
	if err != nil {
		fmt.Fprintf(stderr, "%s: %s: %v\n", binaryName, contextLabel, err)
		return 1
	}
	defer func() { _ = svc.Close() }()
	return fn(svc)
}

func applySkillCLIAction(svc *skills.Service, action string, id string) error {
	switch action {
	case "trust":
		return svc.Trust(id)
	case "quarantine":
		return svc.Quarantine(id)
	case "disable":
		return svc.Disable(id)
	case "enable":
		return svc.SetEnabled(id, true)
	case "remove":
		return svc.Remove(id)
	case "archive":
		return svc.Archive(id)
	case "restore":
		return svc.Restore(id)
	case "pin":
		return svc.Pin(id, true)
	case "unpin":
		return svc.Pin(id, false)
	default:
		return fmt.Errorf("unknown action %s", action)
	}
}

func printSkills(w io.Writer, binaryName string, items []skills.Skill) {
	if len(items) == 0 {
		fmt.Fprintf(w, "%s: skills: none\n", binaryName)
		return
	}
	for _, item := range items {
		status := "disabled"
		if item.Enabled {
			status = "enabled"
		}
		fmt.Fprintf(w, "%s: skill %s [%s/%s/%s] %s\n", binaryName, item.ID, item.TrustState, status, item.State, item.Description)
	}
}

func printSkillsUsage(w io.Writer, binaryName string) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintf(w, "  %s skills list\n", binaryName)
	fmt.Fprintf(w, "  %s skills search QUERY\n", binaryName)
	fmt.Fprintf(w, "  %s skills show ID\n", binaryName)
	fmt.Fprintf(w, "  %s skills install PATH\n", binaryName)
	fmt.Fprintf(w, "  %s skills trust|quarantine|enable|disable|remove ID\n", binaryName)
	fmt.Fprintf(w, "  %s skills archive|restore|pin|unpin ID\n", binaryName)
	fmt.Fprintf(w, "  %s skills usage\n", binaryName)
	fmt.Fprintf(w, "  %s skills curator\n", binaryName)
}
