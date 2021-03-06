package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo"
	"github.com/leandermelms/fhir/models"
	"github.com/leandermelms/fhir/search"
)

// ResourceController provides the necessary CRUD handlers for a given resource.
type ResourceController struct {
	Name string
	DAL  DataAccessLayer
}

// NewResourceController creates a new resource controller for the passed in resource name and the passed in
// DataAccessLayer.
func NewResourceController(name string, dal DataAccessLayer) *ResourceController {
	return &ResourceController{
		Name: name,
		DAL:  dal,
	}
}

func IndexHandler(c echo.Context) error {
	return c.NoContent(http.StatusInternalServerError)
}

// IndexHandler handles requests to list resource instances or search for them.
func (rc *ResourceController) IndexHandler(c echo.Context) error {
	defer func() {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case *search.Error:
				c.JSON(http.StatusOK, x.OperationOutcome)
			default:
				outcome := models.NewOperationOutcome("fatal", "exception", "")
				c.JSON(http.StatusInternalServerError, outcome)
			}
		}
	}()

	searchQuery := search.Query{Resource: rc.Name, Query: c.Request().URL.RawQuery}
	baseURL := responseURL(c.Request(), rc.Name)
	bundle, err := rc.DAL.Search(*baseURL, searchQuery)
	if err != nil {
		return err
	}

	c.Set("bundle", bundle)
	c.Set("Resource", rc.Name)
	c.Set("Action", "search")

	return c.JSON(http.StatusOK, bundle)
}

// LoadResource uses the resource id in the request to get a resource from the DataAccessLayer and store it in the
// context.
func (rc *ResourceController) LoadResource(c echo.Context) (interface{}, error) {
	result, err := rc.DAL.Get(c.Param("id"), rc.Name)
	if err != nil {
		return nil, err
	}

	c.Set(rc.Name, result)
	c.Set("Resource", rc.Name)
	return result, nil
}

// ShowHandler handles requests to get a particular resource by ID.
func (rc *ResourceController) ShowHandler(c echo.Context) error {
	c.Set("Action", "read")
	_, err := rc.LoadResource(c)
	if err != nil && err != ErrNotFound {
		return err
	}

	if err == ErrNotFound {
		return c.NoContent(http.StatusNotFound)
	}
	resource := c.Get(rc.Name)
	return c.JSON(http.StatusOK, resource)
}

// CreateHandler handles requests to create a new resource instance, assigning it a new ID.
func (rc *ResourceController) CreateHandler(c echo.Context) error {
	resource := models.NewStructForResourceName(rc.Name)
	err := FHIRBind(c, resource)
	if err != nil {
		oo := models.NewOperationOutcome("fatal", "exception", err.Error())
		return c.JSON(http.StatusBadRequest, oo)
	}

	id, err := rc.DAL.Post(resource)
	if err != nil {
		return err
	}

	c.Set(rc.Name, resource)
	c.Set("Resource", rc.Name)
	c.Set("Action", "create")

	c.Response().Header().Set("Location", responseURL(c.Request(), rc.Name, id).String())
	return c.JSON(http.StatusCreated, resource)
}

// UpdateHandler handles requests to update a resource having a given ID.  If the resource with that ID does not
// exist, a new resource is created with that ID.
func (rc *ResourceController) UpdateHandler(c echo.Context) error {
	resource := models.NewStructForResourceName(rc.Name)
	err := FHIRBind(c, resource)
	if err != nil {
		oo := models.NewOperationOutcome("fatal", "exception", err.Error())
		return c.JSON(http.StatusBadRequest, oo)
	}

	createdNew, err := rc.DAL.Put(c.Param("id"), resource)
	if err != nil {
		return err
	}

	c.Set(rc.Name, resource)
	c.Set("Resource", rc.Name)

	c.Response().Header().Set("Location", responseURL(c.Request(), rc.Name, c.Param("id")).String())
	if createdNew {
		c.Set("Action", "create")
		return c.JSON(http.StatusCreated, resource)
	} else {
		c.Set("Action", "update")
		return c.JSON(http.StatusOK, resource)
	}
}

// ConditionalUpdateHandler handles requests for conditional updates.  These requests contain search criteria for the
// resource to update.  If the criteria results in no found resources, a new resource is created.  If the criteria
// results in one found resource, that resource will be updated.  Criteria resulting in more than one found resource
// is considered an error.
func (rc *ResourceController) ConditionalUpdateHandler(c echo.Context) error {
	resource := models.NewStructForResourceName(rc.Name)
	err := FHIRBind(c, resource)
	if err != nil {
		oo := models.NewOperationOutcome("fatal", "exception", err.Error())
		return c.JSON(http.StatusBadRequest, oo)
	}

	query := search.Query{Resource: rc.Name, Query: c.Request().URL.RawQuery}
	id, createdNew, err := rc.DAL.ConditionalPut(query, resource)
	if err == ErrMultipleMatches {
		return err
	} else if err != nil {
		return err
	}

	c.Set("Resource", rc.Name)

	c.Response().Header().Set("Location", responseURL(c.Request(), rc.Name, id).String())
	if createdNew {
		c.Set("Action", "create")
		return c.JSON(http.StatusCreated, resource)
	} else {
		c.Set("Action", "update")
		return c.JSON(http.StatusOK, resource)
	}
}

// DeleteHandler handles requests to delete a resource instance identified by its ID.
func (rc *ResourceController) DeleteHandler(c echo.Context) error {
	id := c.Param("id")

	if err := rc.DAL.Delete(id, rc.Name); err != nil && err != ErrNotFound {
		return err
	}

	c.Set(rc.Name, id)
	c.Set("Resource", rc.Name)
	c.Set("Action", "delete")

	return c.NoContent(http.StatusNoContent)
}

// ConditionalDeleteHandler handles requests to delete resources identified by search criteria.  All resources
// matching the search criteria will be deleted.
func (rc *ResourceController) ConditionalDeleteHandler(c echo.Context) error {
	query := search.Query{Resource: rc.Name, Query: c.Request().URL.RawQuery}
	_, err := rc.DAL.ConditionalDelete(query)
	if err != nil {
		return err
	}

	c.Set("Resource", rc.Name)
	c.Set("Action", "delete")

	return c.NoContent(http.StatusNoContent)
}

func responseURL(r *http.Request, paths ...string) *url.URL {
	responseURL := url.URL{}
	if r.TLS == nil {
		responseURL.Scheme = "http"
	} else {
		responseURL.Scheme = "https"
	}
	responseURL.Host = r.Host
	responseURL.Path = fmt.Sprintf("/%s", strings.Join(paths, "/"))

	return &responseURL
}
