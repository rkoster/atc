package getbuild

import (
	"errors"
	"html/template"
	"net/http"

	"github.com/concourse/atc"
	"github.com/concourse/atc/db"
	"github.com/concourse/atc/web"
	"github.com/concourse/atc/web/group"
	"github.com/pivotal-golang/lager"
)

type server struct {
	logger        lager.Logger
	clientFactory web.ClientFactory
	template      *template.Template
}

func NewServer(logger lager.Logger, clientFactory web.ClientFactory, template *template.Template) *server {
	return &server{
		logger:        logger,
		clientFactory: clientFactory,
		template:      template,
	}
}

type TemplateData struct {
	GroupStates []group.State
	Job         atc.Job
	Builds      []atc.Build

	Build        atc.Build
	Inputs       []atc.PublicBuildInput
	PipelineName string
}

func (server *server) getNames(r *http.Request) (string, string, string, error) {
	pipelineName := r.FormValue(":pipeline_name")
	jobName := r.FormValue(":job")
	buildName := r.FormValue(":build")

	if len(pipelineName) == 0 || len(jobName) == 0 || len(buildName) == 0 {
		return pipelineName, jobName, buildName, errors.New("Missing required parameters")
	}

	return pipelineName, jobName, buildName, nil
}

func (server *server) GetBuild(pipelineDB db.PipelineDB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client := server.clientFactory.Build(r)

		pipelineName, jobName, buildName, err := server.getNames(r)
		if err != nil {
			server.logger.Error("failed-to-get-names", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		job, found, err := client.Job(pipelineName, jobName)
		if err != nil {
			server.logger.Error("failed-to-load-job", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		log := server.logger.Session("get-build", lager.Data{
			"job":   job.Name,
			"build": buildName,
		})

		requestedBuild, found, err := client.JobBuild(pipelineName, jobName, buildName)
		if err != nil {
			log.Error("get-build-failed", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		buildInputsOutputs, _, err := client.BuildResources(requestedBuild.ID)
		if err != nil {
			log.Error("failed-to-get-build-resources", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		bs, _, err := client.JobBuilds(pipelineName, jobName)
		if err != nil {
			log.Error("get-all-builds-failed", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		pipeline, _, err := client.Pipeline(pipelineName)
		if err != nil {
			log.Error("get-pipeline-failed", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		templateData := TemplateData{
			GroupStates: group.States(pipeline.Groups, func(g atc.GroupConfig) bool {
				for _, groupJob := range g.Jobs {
					if groupJob == job.Name {
						return true
					}
				}

				return false
			}),

			Job:    job,
			Builds: bs,

			Build:        requestedBuild,
			Inputs:       buildInputsOutputs.Inputs,
			PipelineName: pipelineDB.GetPipelineName(),
		}

		err = server.template.Execute(w, templateData)
		if err != nil {
			log.Fatal("failed-to-build-template", err, lager.Data{
				"template-data": templateData,
			})
		}
	})
}
