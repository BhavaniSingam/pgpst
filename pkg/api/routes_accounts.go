package api

import (
	"time"

	"github.com/pgpst/pgpst/internal/github.com/asaskevich/govalidator"
	r "github.com/pgpst/pgpst/internal/github.com/dancannon/gorethink"
	"github.com/pgpst/pgpst/internal/github.com/dchest/uniuri"
	"github.com/pgpst/pgpst/internal/github.com/gin-gonic/gin"

	"github.com/pgpst/pgpst/pkg/models"
	"github.com/pgpst/pgpst/pkg/utils"
)

func (a *API) createAccount(c *gin.Context) {
	// Decode the input
	var input struct {
		Action   string `json:"action"`
		Username string `json:"username"`
		AltEmail string `json:"alt_email"`
		Token    string `json:"token"`
		Password string `json:"password"`
		Address  string `json:"address"`
	}
	if err := c.Bind(&input); err != nil {
		c.JSON(422, &gin.H{
			"code":    0,
			"message": err.Error(),
		})
		return
	}

	// Switch the action
	switch input.Action {
	case "reserve":
		// Parameters:
		//  - username  - desired username
		//  - alt_email - desired email

		// Normalize the username
		styledID := utils.NormalizeUsername(input.Username)
		nu := utils.RemoveDots(styledID)

		// Validate input:
		// - len(username) >= 3 && len(username) <= 32
		// - email.match(alt_email)
		errors := []string{}
		if len(nu) < 3 {
			errors = append(errors, "Username too short. It must be 3-32 characters long.")
		}
		if len(nu) > 32 {
			errors = append(errors, "Username too long. It must be 3-32 characters long.")
		}
		if !govalidator.IsEmail(input.AltEmail) {
			errors = append(errors, "Invalid alternative e-mail format.")
		}
		if len(errors) > 0 {
			c.JSON(422, &gin.H{
				"code":    0,
				"message": "Validation failed.",
				"errors":  errors,
			})
			return
		}

		// Check in the database whether you can register such account
		cursor, err := r.Table("addresses").Get(nu + "@pgp.st").Ne(nil).Do(func(left r.Term) map[string]interface{} {
			return map[string]interface{}{
				"username":  left,
				"alt_email": r.Table("accounts").GetAllByIndex("alt_email", input.AltEmail).Count().Eq(1),
			}
		}).Run(a.Rethink)
		if err != nil {
			c.JSON(500, &gin.H{
				"code":    0,
				"message": err.Error(),
			})
			return
		}
		defer cursor.Close()
		var result struct {
			Username bool `gorethink:"username"`
			AltEmail bool `gorethink:"alt_email"`
		}
		if err := cursor.One(&result); err != nil {
			c.JSON(500, &gin.H{
				"code":    0,
				"message": err.Error(),
			})
			return
		}
		if result.Username || result.AltEmail {
			errors := []string{}
			if result.Username {
				errors = append(errors, "This username is taken.")
			}
			if result.AltEmail {
				errors = append(errors, "This email address is used.")
			}
			c.JSON(422, &gin.H{
				"code":    0,
				"message": "Naming conflict",
				"errors":  errors,
			})
			return
		}

		// Create an account and an address
		address := &models.Address{
			ID:          nu + "@pgp.st",
			StyledID:    styledID + "@pgp.st",
			DateCreated: time.Now(),
			Owner:       "", // we set it later
		}
		account := &models.Account{
			ID:           uniuri.NewLen(uniuri.UUIDLen),
			DateCreated:  time.Now(),
			MainAddress:  address.ID,
			Subscription: "beta",
			AltEmail:     input.AltEmail,
			Status:       "inactive",
		}
		address.Owner = account.ID

		// Insert them into the database
		if err := r.Table("addresses").Insert(address).Exec(a.Rethink); err != nil {
			c.JSON(500, &gin.H{
				"code":    0,
				"message": err.Error(),
			})
			return
		}
		if err := r.Table("accounts").Insert(account).Exec(a.Rethink); err != nil {
			c.JSON(500, &gin.H{
				"code":    0,
				"message": err.Error(),
			})
			return
		}

		// Write a response
		c.JSON(201, account)
		return
	case "activate":
		// Parameters:
		//  - address - expected address
		//  - token   - relevant token for address

		errors := []string{}

		if !govalidator.IsEmail(input.Address) {
			errors = append(errors, "Invalid address format")
		}
		if input.Token == "" {
			errors = append(errors, "Token is missing")
		}
		if len(errors) > 0 {
			c.JSON(422, &gin.H{
				"code":    0,
				"message": "Validation failed",
				"errors":  errors,
			})
			return
		}

		// Normalise input.Address
		input_address := utils.RemoveDots(utils.NormalizeAddress(input.Address))

		// Check in the database whether these both exist
		cursor, err := r.Table("tokens").Get(input.Token).Do(func(left r.Term) r.Term {
			return r.Branch(
				left.Ne(nil).And(left.Field("type").Eq("activate")),
				map[string]interface{}{
					"token":   left,
					"account": r.Table("accounts").Get(left.Field("owner")),
				},
				map[string]interface{}{
					"token":   nil,
					"account": nil,
				},
			)
		}).Run(a.Rethink)
		if err != nil {
			c.JSON(500, &gin.H{
				"code":    0,
				"message": err.Error(),
			})
			return
		}
		defer cursor.Close()
		var result struct {
			Token   *models.Token   `gorethink:"token"`
			Account *models.Account `gorethink:"account"`
		}
		if err := cursor.One(&result); err != nil {
			c.JSON(500, &gin.H{
				"code":    0,
				"message": err.Error(),
			})
			return
		}
		if result.Token == nil {
			c.JSON(422, &gin.H{
				"code":    0,
				"message": "Activation failed",
				"errors":  []string{"Invalid token"},
			})
			return
		}
		if result.Account.MainAddress != input_address {
			c.JSON(422, &gin.H{
				"code":    0,
				"message": "Activation failed",
				"errors":  []string{"Address does not match token"},
			})
			return
		}

		// Everything seems okay, let's go ahead and delete the token
		if err := r.Table("tokens").Get(result.Token.ID).Delete().Exec(a.Rethink); err != nil {
			c.JSON(500, &gin.H{
				"code":    0,
				"message": err.Error(),
			})
			return
		}

		// token has been deleted now, let's do some post deletion checks
		if result.Token.IsExpired() {
			errors = append(errors, "Token expired")
		}
		if result.Account.Status != "inactive" {
			errors = append(errors, "Account already active")
		}
		if len(errors) > 0 {
			c.JSON(422, &gin.H{
				"code":    0,
				"message": "Activation failed",
				"errors":  errors,
			})
			return
		}

		// make the account active, welcome to pgp.st, new guy!
		if err := r.Table("accounts").Get(result.Account.ID).Update(map[string]interface{}{
			"status":        "active",
			"date_modified": time.Now(),
		}).Exec(a.Rethink); err != nil {
			c.JSON(500, &gin.H{
				"code":    0,
				"message": err.Error(),
			})
			return
		}

		// Create labels for that user
		if err := r.Table("labels").Insert([]*models.Label{
			&models.Label{
				ID:           uniuri.NewLen(uniuri.UUIDLen),
				DateCreated:  time.Now(),
				DateModified: time.Now(),
				Owner:        result.Account.ID,
				Name:         "Inbox",
				System:       true,
			},
			&models.Label{
				ID:           uniuri.NewLen(uniuri.UUIDLen),
				DateCreated:  time.Now(),
				DateModified: time.Now(),
				Owner:        result.Account.ID,
				Name:         "Spam",
				System:       true,
			},
			&models.Label{
				ID:           uniuri.NewLen(uniuri.UUIDLen),
				DateCreated:  time.Now(),
				DateModified: time.Now(),
				Owner:        result.Account.ID,
				Name:         "Sent",
				System:       true,
			},
			&models.Label{
				ID:           uniuri.NewLen(uniuri.UUIDLen),
				DateCreated:  time.Now(),
				DateModified: time.Now(),
				Owner:        result.Account.ID,
				Name:         "Starred",
				System:       true,
			},
		}).Exec(a.Rethink); err != nil {
			c.JSON(500, &gin.H{
				"code":    0,
				"message": err.Error(),
			})
			return
		}

		// Temporary response...
		c.JSON(201, &gin.H{
			"id":      result.Account.ID,
			"message": "Activation successful",
		})
		return
	}

	// Same as default in the switch
	c.JSON(422, &gin.H{
		"code":    0,
		"message": "Validation failed",
		"errors":  []string{"Invalid action"},
	})
	return
}

func (a *API) readAccount(c *gin.Context) {
	// Get token and account info from the context
	var (
		ownAccount = c.MustGet("account").(*models.Account)
		token      = c.MustGet("token").(*models.Token)
	)

	// Resolve the account ID and check scope accordingly
	id := c.Param("id")
	if id == "me" {
		// Swap the ID to own account's ID
		id = ownAccount.ID
	}

	if id == ownAccount.ID {
		// Check the scope
		if !models.InScope(token.Scope, []string{"account:read"}) {
			c.JSON(403, &gin.H{
				"code":  0,
				"error": "Your token has insufficient scope",
			})
			return
		}

		// Get addresses from database
		cursor, err := r.Table("addresses").GetAllByIndex("owner", id).Run(a.Rethink)
		if err != nil {
			c.JSON(500, &gin.H{
				"code":  0,
				"error": err.Error(),
			})
			return
		}
		defer cursor.Close()
		var addresses []*models.Address
		if err := cursor.All(&addresses); err != nil {
			c.JSON(500, &gin.H{
				"code":  0,
				"error": err.Error(),
			})
			return
		}

		ownAccount.Password = nil

		// Write the response
		c.JSON(200, struct {
			*models.Account
			Addresses []*models.Address `json:"addresses"`
		}{
			Account:   ownAccount,
			Addresses: addresses,
		})
		return
	}
	// Check the scope
	if !models.InScope(token.Scope, []string{"admin"}) {
		c.JSON(403, &gin.H{
			"code":  0,
			"error": "Your token has insufficient scope",
		})
		return
	}

	// Fetch the account
	cursor, err := r.Table("accounts").Get(id).Do(func(account r.Term) r.Term {
		return r.Branch(
			account.Eq(nil),
			map[string]interface{}{},
			account.Without("password").Merge(map[string]interface{}{
				"addresses": r.Table("addresses").GetAllByIndex("owner", account.Field("id")),
			}),
		)
	}).Run(a.Rethink)
	if err != nil {
		c.JSON(500, &gin.H{
			"code":  0,
			"error": err.Error(),
		})
		return
	}
	defer cursor.Close()
	var result struct {
		models.Account
		Addresses []*models.Address `json:"addresses"`
	}
	if err := cursor.One(&result); err != nil {
		c.JSON(500, &gin.H{
			"code":  0,
			"error": err.Error(),
		})
		return
	}

	if result.ID == "" {
		c.JSON(404, &gin.H{
			"code":  0,
			"error": "Account not found",
		})
		return
	}

	c.JSON(200, result)
}

func (a *API) updateAccount(c *gin.Context) {
	// Get token and account info from the context
	var (
		account = c.MustGet("account").(*models.Account)
		token   = c.MustGet("token").(*models.Token)
	)

	// Decode the input
	var input struct {
		MainAddress string `json:"main_address"`
		NewPassword []byte `json:"new_password"`
		OldPassword []byte `json:"old_password"`
	}
	if err := c.Bind(&input); err != nil {
		c.JSON(422, &gin.H{
			"code":    0,
			"message": err.Error(),
		})
		return
	}

	var newAddress *models.Address
	if input.MainAddress != "" {
		// Fetch address from the database
		cursor, err := r.Table("addresses").Get(input.MainAddress).Default(map[string]interface{}{}).Run(a.Rethink)
		if err != nil {
			c.JSON(500, &gin.H{
				"code":    0,
				"message": err.Error(),
			})
			return
		}
		defer cursor.Close()
		if err := cursor.One(&newAddress); err != nil {
			c.JSON(500, &gin.H{
				"code":    0,
				"message": err.Error(),
			})
			return
		}

		// Verify that we got something
		if newAddress.ID == "" {
			c.JSON(422, &gin.H{
				"code":    0,
				"message": "No such address exists",
			})
			return
		}
	}

	// Resolve the account ID and check scope accordingly
	id := c.Param("id")
	if id == "me" {
		// Swap the ID to own account's ID
		id = account.ID
	}

	if id == account.ID {
		// Check the scope
		if !models.InScope(token.Scope, []string{"account:modify"}) {
			c.JSON(403, &gin.H{
				"code":  0,
				"error": "Your token has insufficient scope",
			})
			return
		}

		// Validate password
		valid, _, err := account.VerifyPassword(input.OldPassword)
		if err != nil {
			c.JSON(500, &gin.H{
				"code":    0,
				"message": err.Error(),
			})
			return
		}
		if !valid {
			c.JSON(401, &gin.H{
				"code":    0,
				"message": "Invalid password",
			})
			return
		}
	} else {
		// Check the scope
		if !models.InScope(token.Scope, []string{"admin"}) {
			c.JSON(403, &gin.H{
				"code":  0,
				"error": "Your token has insufficient scope",
			})
			return
		}

		// Fetch the account from the database
		cursor, err := r.Table("accounts").Get(id).Default(map[string]interface{}{}).Run(a.Rethink)
		if err != nil {
			c.JSON(500, &gin.H{
				"code":    0,
				"message": err.Error(),
			})
			return
		}
		defer cursor.Close()
		if err := cursor.One(&account); err != nil {
			c.JSON(500, &gin.H{
				"code":    0,
				"message": err.Error(),
			})
			return
		}

		// Ensure that we got something
		if account.ID == "" {
			c.JSON(404, &gin.H{
				"code":  0,
				"error": "Account not found",
			})
			return
		}
	}

	// Apply change to the password
	if input.NewPassword != nil && len(input.NewPassword) != 0 {
		if len(input.NewPassword) == 32 {
			c.JSON(422, &gin.H{
				"code":    0,
				"message": "Invalid new password format",
			})
			return
		}

		if err := account.SetPassword(input.NewPassword); err != nil {
			c.JSON(500, &gin.H{
				"code":    0,
				"message": err.Error(),
			})
			return
		}
	}

	// Apply change to the main address setting
	if newAddress.ID != "" {
		// Check address ownership
		if newAddress.Owner != account.ID {
			c.JSON(422, &gin.H{
				"code":    0,
				"message": "User does not own that address",
			})
			return
		}

		account.MainAddress = newAddress.ID
	}

	// Perform the update
	if err := r.Table("accounts").Update(account).Exec(a.Rethink); err != nil {
		c.JSON(500, &gin.H{
			"code":    0,
			"message": err.Error(),
		})
		return
	}

	// Write the response
	account.Password = nil
	c.JSON(200, account)
}
