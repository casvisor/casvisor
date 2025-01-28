// Copyright 2024 The casbin Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package object

import (
	"fmt"

	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

type Image struct {
	Owner    string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name     string `xorm:"varchar(100) notnull pk" json:"name"`
	Category string `xorm:"varchar(100)" json:"category"`

	ImageId     string `xorm:"varchar(100)" json:"id"`
	State       string `xorm:"varchar(100)" json:"state"`
	Tag         string `xorm:"varchar(100)" json:"tag"`
	Description string `xorm:"varchar(100)" json:"description"`

	Os                 string `xorm:"varchar(100)" json:"os"`
	Platform           string `xorm:"varchar(100)" json:"platform"`
	SystemArchitecture string `xorm:"varchar(100)" json:"systemArchitecture"`
	Size               string `xorm:"varchar(100)" json:"size"`
	StartupMode        string `xorm:"varchar(100)" json:"startupMode"`
	Progress           string `xorm:"varchar(100)" json:"progress"`

	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`
}

func GetImageCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&Image{})
}

func GetImages(owner string) ([]*Image, error) {
	images := []*Image{}
	err := adapter.engine.Desc("created_time").Find(&images, &Image{Owner: owner})
	if err != nil {
		return images, err
	}

	return images, nil
}

func GetPaginationImages(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*Image, error) {
	images := []*Image{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&images)
	if err != nil {
		return images, err
	}

	return images, nil
}

func getImage(owner string, name string) (*Image, error) {
	if owner == "" || name == "" {
		return nil, nil
	}

	image := Image{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&image)
	if err != nil {
		return &image, err
	}

	if existed {
		return &image, nil
	} else {
		return nil, nil
	}
}

func GetImage(id string) (*Image, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getImage(owner, name)
}

func GetMaskedImage(image *Image, errs ...error) (*Image, error) {
	if len(errs) > 0 && errs[0] != nil {
		return nil, errs[0]
	}

	if image == nil {
		return nil, nil
	}

	//if image.ImageId != "" {
	//	image.ImageId = "***"
	//}
	return image, nil
}

func GetMaskedImages(images []*Image, errs ...error) ([]*Image, error) {
	if len(errs) > 0 && errs[0] != nil {
		return nil, errs[0]
	}

	var err error
	for _, image := range images {
		image, err = GetMaskedImage(image)
		if err != nil {
			return nil, err
		}
	}

	return images, nil
}

func UpdateImage(id string, image *Image) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	p, err := getImage(owner, name)
	if err != nil {
		return false, err
	} else if p == nil {
		return false, nil
	}

	//if image.ImageId == "***" {
	//	image.ImageId = p.ImageId
	//}

	affected, err := adapter.engine.ID(core.PK{owner, name}).AllCols().Update(image)
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func AddImage(image *Image) (bool, error) {
	affected, err := adapter.engine.Insert(image)
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func DeleteImage(image *Image) (bool, error) {
	affected, err := adapter.engine.ID(core.PK{image.Owner, image.Name}).Delete(&Image{})
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func (image *Image) getId() string {
	return fmt.Sprintf("%s/%s", image.Owner, image.Name)
}
