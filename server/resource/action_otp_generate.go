package resource

import (
	"context"
	"errors"
	"github.com/artpar/api2go"
	"github.com/daptin/daptin/server/auth"
	daptinid "github.com/daptin/daptin/server/id"
	"github.com/jmoiron/sqlx"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	log "github.com/sirupsen/logrus"
	"net/http"
	"time"
)

type otpGenerateActionPerformer struct {
	responseAttrs    map[string]interface{}
	cruds            map[string]*DbResource
	configStore      *ConfigStore
	encryptionSecret []byte
}

func (actionPerformer *otpGenerateActionPerformer) Name() string {
	return "otp.generate"
}

func (actionPerformer *otpGenerateActionPerformer) DoAction(request Outcome, inFieldMap map[string]interface{}, transaction *sqlx.Tx) (api2go.Responder, []ActionResponse, []error) {

	email, emailOk := inFieldMap["email"]
	mobile, phoneOk := inFieldMap["mobile"]
	var userAccount map[string]interface{}
	var userOtpProfile map[string]interface{}
	var err error

	if !emailOk && !phoneOk {
		return nil, nil, []error{errors.New("email or mobile missing")}
	} else if emailOk && email != "" {
		userAccount, err = actionPerformer.cruds["user_account"].GetUserAccountRowByEmail(email.(string), transaction)
		if (err != nil || userAccount == nil) && !phoneOk {
			return nil, nil, []error{errors.New("invalid email")}
		}
		i := userAccount["id"]
		if i == nil {
			return nil, nil, []error{errors.New("invalid account")}
		}
		userOtpProfile, err = actionPerformer.cruds["user_otp_account"].GetObjectByWhereClause("user_otp_account", "otp_of_account", i.(int64), transaction)
	}

	if phoneOk && userAccount == nil && mobile != "" {
		userOtpProfile, err = actionPerformer.cruds["user_otp_account"].GetObjectByWhereClause("user_otp_account", "mobile_number", mobile, transaction)
		if err != nil {
			return nil, nil, []error{errors.New("unregistered number")}
		}
		i := userOtpProfile["otp_of_account"]
		if i == nil {
			return nil, nil, []error{errors.New("unregistered number")}
		}
		userAccount, _, err = actionPerformer.cruds["user_account"].GetSingleRowByReferenceIdWithTransaction("user_account", i.(daptinid.DaptinReferenceId), nil, transaction)
		if err != nil {
			return nil, nil, []error{errors.New("unregistered number")}
		}
	}

	if mobile == nil {
		mobile = ""
		phoneOk = true
	}

	httpReq := &http.Request{}
	user := &auth.SessionUser{
		UserId:          userAccount["id"].(int64),
		UserReferenceId: userAccount["reference_id"].(daptinid.DaptinReferenceId),
	}
	httpReq = httpReq.WithContext(context.WithValue(context.Background(), "user", user))
	req := api2go.Request{
		PlainRequest: httpReq,
	}

	if userOtpProfile == nil {

		key, err := totp.Generate(totp.GenerateOpts{
			Issuer:      "site.daptin.com",
			AccountName: userAccount["email"].(string),
			Period:      300,
			Digits:      4,
			SecretSize:  10,
		})

		if err != nil {
			log.Errorf("Failed to generate code: %v", err)
			return nil, nil, []error{err}
		}

		userOtpProfile = map[string]interface{}{
			"otp_secret":     key.Secret(),
			"verified":       0,
			"mobile_number":  mobile,
			"otp_of_account": userAccount["reference_id"],
		}

		req.PlainRequest.Method = "POST"
		createdOtpProfile, err := actionPerformer.cruds["user_otp_account"].CreateWithoutFilter(api2go.NewApi2GoModelWithData("user_otp_account",
			nil, 0, nil, userOtpProfile), req, transaction)
		if err != nil {
			return nil, nil, []error{errors.New("failed to create otp profile")}
		}

		userOtpProfile = createdOtpProfile
	}

	if userOtpProfile["verified"] == 1 && phoneOk && mobile != userOtpProfile["mobile_number"] && mobile != "" {
		userOtpProfile["mobile_number"] = mobile
		userOtpProfile["verified"] = 0
		req.PlainRequest.Method = "PUT"
		if err != nil {
			return nil, nil, []error{err}
		}
		_, err = actionPerformer.cruds["user_otp_account"].UpdateWithoutFilters(
			api2go.NewApi2GoModelWithData("user_otp_account", nil, 0, nil, userOtpProfile), req, transaction)
		if err != nil {
			return nil, nil, []error{err}
		} else {
			return nil, nil, nil
		}
	}

	resp := &api2go.Response{}
	if userOtpProfile["verified"] == 1 || phoneOk {

		key, err := Decrypt(actionPerformer.encryptionSecret, userOtpProfile["otp_secret"].(string))
		if err != nil {
			return nil, []ActionResponse{NewActionResponse("client.notify", NewClientNotification("message", "Failed to generate new OTP code", "Failed"))}, []error{err}
		}

		state, err := totp.GenerateCodeCustom(key, time.Now(), totp.ValidateOpts{
			Period:    300,
			Skew:      1,
			Digits:    4,
			Algorithm: otp.AlgorithmSHA1,
		})
		if err != nil {
			log.Errorf("Failed to generate code: %v", err)
			return nil, []ActionResponse{NewActionResponse("client.notify", NewClientNotification("message", "Failed to generate new OTP code", "Failed"))}, []error{err}
		}

		responder := api2go.NewApi2GoModelWithData("otp", nil, 0, nil, map[string]interface{}{
			"otp": state,
		})
		resp.Res = responder
	} else {
		resp = nil
	}

	return resp, []ActionResponse{}, []error{err}
}

func NewOtpGenerateActionPerformer(cruds map[string]*DbResource, configStore *ConfigStore, transaction *sqlx.Tx) (ActionPerformerInterface, error) {

	encryptionSecret, _ := configStore.GetConfigValueFor("encryption.secret", "backend", transaction)

	handler := otpGenerateActionPerformer{
		cruds:            cruds,
		encryptionSecret: []byte(encryptionSecret),
		configStore:      configStore,
	}

	return &handler, nil

}
