package build

import (
	"bytes"
	"os"
	"path"
	"sort"
	"text/template"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/modem7/docker-error-pages/internal/config"
	"github.com/modem7/docker-error-pages/internal/tpl"
	"go.uber.org/zap"
)

type historyItem struct {
	Code, Message, Path string
}

// NewCommand creates `build` command.
func NewCommand(log *zap.Logger, configFile *string) *cobra.Command { //nolint:funlen,gocognit,gocyclo
	var (
		generateIndex bool
		cfg           *config.Config
	)

	cmd := &cobra.Command{
		Use:     "build <output-directory>",
		Aliases: []string{"b"},
		Short:   "Build the error pages",
		Args:    cobra.ExactArgs(1),
		PreRunE: func(*cobra.Command, []string) error {
			if configFile == nil {
				return errors.New("path to the config file is required for this command")
			}

			if c, err := config.FromYamlFile(*configFile); err != nil {
				return err
			} else {
				if err = c.Validate(); err != nil {
					return err
				}

				cfg = c
			}

			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("wrong arguments count")
			}

			errorPages := tpl.NewErrorPages()

			log.Info("loading templates")
			if templates, err := cfg.LoadTemplates(); err == nil {
				if len(templates) > 0 {
					for templateName, content := range templates {
						errorPages.AddTemplate(templateName, content)
					}

					for code, desc := range cfg.Pages {
						errorPages.AddPage(code, desc.Message, desc.Description)
					}
				} else {
					return errors.New("no loaded templates")
				}
			} else {
				return err
			}

			log.Debug("the output directory preparing", zap.String("Path", args[0]))
			if err := createDirectory(args[0]); err != nil {
				return errors.Wrap(err, "cannot prepare output directory")
			}

			history, startedAt := make(map[string][]historyItem), time.Now()

			log.Info("saving the error pages")
			if err := errorPages.IteratePages(func(template, code string, content []byte) error {
				if e := createDirectory(path.Join(args[0], template)); e != nil {
					return e
				}

				fileName := code + ".html"

				if e := os.WriteFile(path.Join(args[0], template, fileName), content, 0664); e != nil { //nolint:gosec,gomnd
					return e
				}

				if _, ok := history[template]; !ok {
					history[template] = make([]historyItem, 0, len(cfg.Pages))
				}

				history[template] = append(history[template], historyItem{
					Code:    code,
					Message: cfg.Pages[code].Message,
					Path:    path.Join(template, fileName),
				})

				return nil
			}); err != nil {
				return err
			}

			log.Debug("saved", zap.Duration("duration", time.Since(startedAt)))

			if generateIndex {
				for _, h := range history {
					sort.Slice(h, func(i, j int) bool {
						return h[i].Code < h[j].Code
					})
				}

				log.Info("index file generation")
				startedAt = time.Now()

				if err := writeIndexFile(path.Join(args[0], "index.html"), history); err != nil {
					return err
				}

				log.Debug("index file generated", zap.Duration("duration", time.Since(startedAt)))
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(
		&generateIndex,
		"index", "i",
		false,
		"generate index page",
	)

	return cmd
}

func createDirectory(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(path, 0775) //nolint:gomnd
		}

		return err
	}

	if !stat.IsDir() {
		return errors.New("is not a directory")
	}

	return nil
}

func writeIndexFile(path string, history map[string][]historyItem) error {
	t, err := template.New("index").Parse(`<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1, shrink-to-fit=no" />
  <title>Error pages list</title>
  <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/twitter-bootstrap/5.1.1/css/bootstrap.min.css"
    integrity="sha512-6KY5s6UI5J7SVYuZB4S/CZMyPylqyyNZco376NM2Z8Sb8OxEdp02e1jkKk/wZxIEmjQ6DRCEBhni+gpr9c4tvA=="
    crossorigin="anonymous" referrerpolicy="no-referrer" />
</head>
<body class="bg-dark text-white">
<div class="container">
  <main>
    <div class="py-5 text-center">
      <img class="d-block mx-auto mb-4" src="https://hsto.org/webt/rm/9y/ww/rm9ywwx3gjv9agwkcmllhsuyo7k.png"
           alt="" width="94">
      <h2>Error pages index</h2>
    </div>
{{- range $template, $item := . -}}
    <h2 class="mb-3">Template name: <Code>{{ $template }}</Code></h2>
    <ul class="mb-5">
	{{ range $item -}}
      <li><a href="{{ .Path }}"><strong>{{ .Code }}</strong>: {{ .Message }}</a></li>
	{{ end -}}
    </ul>
{{ end }}
  </main>
</div>
<footer class="footer">
  <div class="container text-center text-muted mt-3 mb-3">
    For online documentation and support please refer to the
      <a href="https://github.com/modem7/docker-error-pages">project repository</a>.
  </div>
</footer>
</body>
</html>`)
	if err != nil {
		return err
	}

	var buf bytes.Buffer

	if err = t.Execute(&buf, history); err != nil {
		return err
	}

	return os.WriteFile(path, buf.Bytes(), 0664) //nolint:gosec,gomnd
}