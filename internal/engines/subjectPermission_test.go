package engines

import (
	`context`
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	`reflect`

	`github.com/Permify/permify/internal/config`
	`github.com/Permify/permify/internal/factories`
	`github.com/Permify/permify/internal/invoke`
	`github.com/Permify/permify/pkg/database`
	`github.com/Permify/permify/pkg/logger`
	base `github.com/Permify/permify/pkg/pb/base/v1`
	`github.com/Permify/permify/pkg/telemetry`
	`github.com/Permify/permify/pkg/token`
	`github.com/Permify/permify/pkg/tuple`
)

var _ = Describe("subject-permission-engine", func() {

	driveSchema := `
entity user {}

entity organization {
	relation admin @user
}

entity folder {
	relation org @organization
	relation creator @user
	relation collaborator @user

	permission read = collaborator
	permission update = collaborator
	permission delete = creator or org.admin
	permission share = update
}

entity doc {
	relation org @organization
	relation parent @folder

	relation owner @user @organization#admin
	relation member @user

	permission read = owner or member
	permission update = owner and org.admin
	permission delete = owner or org.admin
	permission share = update and (member not parent.update)
}
`

	Context("Drive Sample: Subject Permission", func() {
		It("Drive Sample: Case 1", func() {
			db, err := factories.DatabaseFactory(
				config.Database{
					Engine: "memory",
				},
			)

			Expect(err).ShouldNot(HaveOccurred())

			conf, err := newSchema(driveSchema)
			Expect(err).ShouldNot(HaveOccurred())

			schemaWriter := factories.SchemaWriterFactory(db, logger.New("debug"))
			err = schemaWriter.WriteSchema(context.Background(), conf)

			Expect(err).ShouldNot(HaveOccurred())

			type assertion struct {
				onlyPermission bool
				subject        string
				entity         string
				result         map[string]base.CheckResult
			}

			tests := struct {
				relationships []string
				assertions    []assertion
			}{
				relationships: []string{
					"doc:1#owner@user:2",
					"doc:1#owner@user:1",
					"doc:1#member@user:1",
				},
				assertions: []assertion{
					{
						subject:        "user:1",
						entity:         "doc:1",
						onlyPermission: false,
						result: map[string]base.CheckResult{
							"org":    base.CheckResult_RESULT_DENIED,
							"parent": base.CheckResult_RESULT_DENIED,

							"owner":  base.CheckResult_RESULT_ALLOWED,
							"member": base.CheckResult_RESULT_ALLOWED,

							"read":   base.CheckResult_RESULT_ALLOWED,
							"update": base.CheckResult_RESULT_DENIED,
							"delete": base.CheckResult_RESULT_ALLOWED,
							"share":  base.CheckResult_RESULT_DENIED,
						},
					},
					{
						onlyPermission: true,
						subject:        "user:1",
						entity:         "doc:1",
						result: map[string]base.CheckResult{
							"read":   base.CheckResult_RESULT_ALLOWED,
							"update": base.CheckResult_RESULT_DENIED,
							"delete": base.CheckResult_RESULT_ALLOWED,
							"share":  base.CheckResult_RESULT_DENIED,
						},
					},
				},
			}

			schemaReader := factories.SchemaReaderFactory(db, logger.New("debug"))
			relationshipReader := factories.RelationshipReaderFactory(db, logger.New("debug"))
			relationshipWriter := factories.RelationshipWriterFactory(db, logger.New("debug"))

			checkEngine := NewCheckEngine(schemaReader, relationshipReader)

			subjectPermissionEngine := NewSubjectPermission(checkEngine, schemaReader)

			invoker := invoke.NewDirectInvoker(
				schemaReader,
				relationshipReader,
				checkEngine,
				nil,
				nil,
				nil,
				subjectPermissionEngine,
				telemetry.NewNoopMeter(),
			)

			checkEngine.SetInvoker(invoker)

			var tuples []*base.Tuple

			for _, relationship := range tests.relationships {
				t, err := tuple.Tuple(relationship)
				Expect(err).ShouldNot(HaveOccurred())
				tuples = append(tuples, t)
			}

			_, err = relationshipWriter.WriteRelationships(context.Background(), "t1", database.NewTupleCollection(tuples...))
			Expect(err).ShouldNot(HaveOccurred())

			for _, assertion := range tests.assertions {
				entity, err := tuple.E(assertion.entity)
				Expect(err).ShouldNot(HaveOccurred())

				ear, err := tuple.EAR(assertion.subject)
				Expect(err).ShouldNot(HaveOccurred())

				subject := &base.Subject{
					Type:     ear.GetEntity().GetType(),
					Id:       ear.GetEntity().GetId(),
					Relation: ear.GetRelation(),
				}

				response, err := invoker.SubjectPermission(context.Background(), &base.PermissionSubjectPermissionRequest{
					TenantId: "t1",
					Subject:  subject,
					Entity:   entity,
					Metadata: &base.PermissionSubjectPermissionRequestMetadata{
						SnapToken:      token.NewNoopToken().Encode().String(),
						SchemaVersion:  "",
						Depth:          100,
						OnlyPermission: assertion.onlyPermission,
					},
				})

				Expect(err).ShouldNot(HaveOccurred())
				Expect(reflect.DeepEqual(response.Results, assertion.result)).Should(Equal(true))
			}
		})

	})
})
