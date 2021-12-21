package revolver

import (
	"context"
	"fmt"
	"os"

	_ "github.com/grezar/revolver/provider/from/awsiamuser"
	_ "github.com/grezar/revolver/provider/from/stdin"
	_ "github.com/grezar/revolver/provider/to/awssharedcredentials"
	_ "github.com/grezar/revolver/provider/to/circleci"
	_ "github.com/grezar/revolver/provider/to/stdout"
	_ "github.com/grezar/revolver/provider/to/tfe"
	"github.com/grezar/revolver/reporting"
	"github.com/grezar/revolver/schema"
	"github.com/grezar/revolver/secrets"
)

type Runner struct {
	rotations []*schema.Rotation
	dryRun    bool
}

func NewRunner(path string, dryRun bool) (*Runner, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rotations, err := schema.LoadRotations(f)
	if err != nil {
		return nil, err
	}

	return &Runner{
		rotations: rotations,
		dryRun:    dryRun,
	}, nil
}

func (r *Runner) Run(rptr *reporting.R) {
	for _, rn := range r.rotations {
		rn := rn
		rptr.Run(rn.Name, func(rptr *reporting.R) {
			rptr.Parallel()
			ctx := context.Background()

			rptr.Run(fmt.Sprintf("From/%s", rn.From.Provider), func(rptr *reporting.R) {
				rptr.Summary(rn.From.Spec.Operator.Summary())
				newSecrets, err := rn.From.Spec.Operator.Do(ctx, r.dryRun)
				if err != nil {
					rptr.Fail(err)
					return
				}
				if len(newSecrets) > 0 {
					rptr.Success()
					ctx = secrets.WithSecrets(ctx, newSecrets)
				} else {
					if r.dryRun {
						rptr.Success()
					} else {
						rptr.Skip()
					}
				}
			})

			// Ensure that the cleanup process is invoked when the provider's Do
			// operation succeeds
			defer func() {
				err := rn.From.Spec.Cleanup(ctx, r.dryRun)
				if err != nil {
					rptr.Fail(err)
				}
			}()

			for _, to := range rn.To {
				to := to
				rptr.Run(fmt.Sprintf("To/%s", to.Provider), func(rptr *reporting.R) {
					rptr.Parallel()
					rptr.Summary(to.Spec.Operator.Summary())
					if len(secrets.GetSecrets(ctx)) == 0 && !r.dryRun {
						rptr.Skip()
						return
					}

					err := to.Spec.Operator.Do(ctx, r.dryRun)
					if err != nil {
						rptr.Fail(err)
						return
					}
					rptr.Success()
				})
			}
		})
	}
}
