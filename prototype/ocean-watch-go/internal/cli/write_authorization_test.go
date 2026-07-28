package cli

import "testing"

func TestWriteAuthorizationBoundary(t *testing.T) {
	t.Run("Marketing create flags", func(t *testing.T) {
		for _, action := range []string{"create", "create-creator"} {
			_, preview, err := parseMarketingCreateOptions(action, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, submit, err := parseMarketingCreateOptions(action, []string{"--submit"})
			if err != nil {
				t.Fatal(err)
			}
			if preview.Submit || !submit.Submit {
				t.Fatalf("Marketing %s submit boundary changed: preview=%t submit=%t", action, preview.Submit, submit.Submit)
			}
		}
	})

	t.Run("Marketing batch flags", func(t *testing.T) {
		_, uploadPreview, err := parseMarketingUploadBatchOptions(nil)
		if err != nil {
			t.Fatal(err)
		}
		_, uploadSubmit, err := parseMarketingUploadBatchOptions([]string{"--submit"})
		if err != nil {
			t.Fatal(err)
		}
		creatorPreview, err := parseMarketingCreatorBatchOptions([]string{"--jobs-file", "fixture.json"})
		if err != nil {
			t.Fatal(err)
		}
		creatorSubmit, err := parseMarketingCreatorBatchOptions([]string{"--jobs-file", "fixture.json", "--submit"})
		if err != nil {
			t.Fatal(err)
		}
		if uploadPreview.Submit || !uploadSubmit.Submit || creatorPreview.submit || !creatorSubmit.submit {
			t.Fatalf("Marketing batch submit boundary changed: upload=%t/%t creator=%t/%t",
				uploadPreview.Submit, uploadSubmit.Submit, creatorPreview.submit, creatorSubmit.submit)
		}
	})

	t.Run("Marketing mutation flags", func(t *testing.T) {
		args := []string{"--advertiser-id", "1001", "--promotion-id", "3001", "--value", "500"}
		_, preview, err := parseMarketingMutationOptions("update-budget", args)
		if err != nil {
			t.Fatal(err)
		}
		_, submit, err := parseMarketingMutationOptions("update-budget", append(append([]string(nil), args...), "--submit"))
		if err != nil {
			t.Fatal(err)
		}
		if preview.Submit || !submit.Submit {
			t.Fatalf("Marketing mutation submit boundary changed: preview=%t submit=%t", preview.Submit, submit.Submit)
		}
	})

	t.Run("Qianchuan create and batch flags", func(t *testing.T) {
		createArgs := []string{"--payload-json", qianchuanCLIPayload}
		_, createPreview, err := parseQianchuanCreateOptions(createArgs, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, createSubmit, err := parseQianchuanCreateOptions(append(append([]string(nil), createArgs...), "--submit"), nil)
		if err != nil {
			t.Fatal(err)
		}
		batchArgs := []string{"--plan-template", "fixture-template", "--work-url", "https://www.douyin.com/video/6001"}
		_, batchPreview, err := parseQianchuanBatchOptions(batchArgs)
		if err != nil {
			t.Fatal(err)
		}
		_, batchSubmit, err := parseQianchuanBatchOptions(append(append([]string(nil), batchArgs...), "--submit"))
		if err != nil {
			t.Fatal(err)
		}
		if createPreview.Submit || !createSubmit.Submit || batchPreview.Submit || !batchSubmit.Submit {
			t.Fatalf("Qianchuan create/batch submit boundary changed: create=%t/%t batch=%t/%t",
				createPreview.Submit, createSubmit.Submit, batchPreview.Submit, batchSubmit.Submit)
		}
	})

	t.Run("Qianchuan deletion requires two independent flags", func(t *testing.T) {
		args := []string{
			"--advertiser-id", "1000000000000001", "--ad-id", "2000000000000001",
			"--work-url", "https://www.douyin.com/video/6001",
		}
		_, preview, err := parseQianchuanRemoveOptions(args)
		if err != nil {
			t.Fatal(err)
		}
		_, submitOnly, err := parseQianchuanRemoveOptions(append(append([]string(nil), args...), "--submit"))
		if err != nil {
			t.Fatal(err)
		}
		_, confirmed, err := parseQianchuanRemoveOptions(append(append([]string(nil), args...), "--submit", "--confirm-delete"))
		if err != nil {
			t.Fatal(err)
		}
		if preview.Submit || preview.ConfirmDelete || !submitOnly.Submit || submitOnly.ConfirmDelete ||
			!confirmed.Submit || !confirmed.ConfirmDelete {
			t.Fatalf("Qianchuan delete flags changed: preview=%#v submit=%#v confirmed=%#v", preview, submitOnly, confirmed)
		}
	})

	t.Run("Qianchuan mutation flags", func(t *testing.T) {
		args := []string{"--advertiser-id", "1000000000000001", "--ad-id", "2000000000000001", "--value", "500"}
		_, preview, err := parseQianchuanMutationOptions("update-budget", args)
		if err != nil {
			t.Fatal(err)
		}
		_, submit, err := parseQianchuanMutationOptions("update-budget", append(append([]string(nil), args...), "--submit"))
		if err != nil {
			t.Fatal(err)
		}
		if preview.Submit || !submit.Submit {
			t.Fatalf("Qianchuan mutation submit boundary changed: preview=%t submit=%t", preview.Submit, submit.Submit)
		}
	})
}
