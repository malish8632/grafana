package sqlstore

import (
	"github.com/grafana/grafana/pkg/services/annotations"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"

	m "github.com/grafana/grafana/pkg/models"
)

func addTestAnnotation(dashboard *m.Dashboard, created int64, sess *DBSession) error {
	created *= 1000
	tags := m.ParseTagPairs([]string{"outage", "error", "type:outage", "server:server-1"})

	item := annotations.Item{
		Tags:        m.JoinTagPairs(tags),
		Text:        "hello",
		Type:        "alert",
		UserId:      1,
		OrgId:       dashboard.OrgId,
		DashboardId: dashboard.Id,
		Created:     created,
		Updated:     created,
		Epoch:       created,
		EpochEnd:    created,
	}
	if _, err := sess.Table("annotation").Insert(&item); err != nil {
		return err
	}

	// Get tag IDs
	tags, err := EnsureTagsExist(sess, tags)
	if err != nil {
		return err
	}
	for _, tag := range tags {
		if _, err := sess.Exec("INSERT INTO annotation_tag (annotation_id, tag_id) VALUES(?,?)", item.Id, tag.Id); err != nil {
			return err
		}
	}

	return nil
}

func TestDeleteExpiredAnnotations(t *testing.T) {
	Convey("Deletion of expired annotations", t, func() {
		InitTestDB(t)
		repo := SqlAnnotationRepo{}
		daysToKeepAnnotations := 5
		annotationsToWrite := 10

		expiredCreated := (time.Now().Unix() - int64(daysToKeepAnnotations*2*86400))
		// Add expired annotations
		savedDash := insertTestDashboard("test dash 111", 1, 0, false, "this-is-fun")
		err := inTransaction(func(sess *DBSession) error {
			for i := 0; i < annotationsToWrite-1; i++ {
				if err := addTestAnnotation(savedDash, expiredCreated, sess); err != nil {
					return err
				}
			}

			// Add one recent
			newCreated := (time.Now().Unix() - int64(2*86400))
			return addTestAnnotation(savedDash, newCreated, sess)
		})
		So(err, ShouldBeNil)

		Convey("Clean up expired annotations", func() {
			err := deleteExpiredAnnotations(&m.DeleteExpiredAnnotationsCommand{DaysToKeep: daysToKeepAnnotations})
			So(err, ShouldBeNil)

			items, err := repo.Find(&annotations.ItemQuery{
				OrgId:       1,
				DashboardId: 1,
				From:        expiredCreated,
				To:          time.Now().Unix() * 1000,
			})
			So(err, ShouldBeNil)

			So(len(items), ShouldEqual, 1)
			So(items[0].Tags, ShouldResemble, []string{"outage", "error", "type:outage", "server:server-1"})
		})

		Convey("Don't delete anything if there're no expired versions", func() {
			err := deleteExpiredAnnotations(&m.DeleteExpiredAnnotationsCommand{DaysToKeep: annotationsToWrite})
			So(err, ShouldBeNil)

			items, err := repo.Find(&annotations.ItemQuery{
				OrgId:       1,
				DashboardId: 1,
			})
			So(err, ShouldBeNil)

			So(len(items), ShouldEqual, 1)
		})

		Convey("Don't delete more than MAX_VERSIONS_TO_DELETE per iteration", func() {
			annotationsToWriteBigNumber := MAX_HISTORY_ENTRIES_TO_DELETE + annotationsToWrite
			err := inTransaction(func(sess *DBSession) error {
				created := (time.Now().Unix() - int64(daysToKeepAnnotations*2*86400))
				for i := 0; i < annotationsToWriteBigNumber-annotationsToWrite; i++ {
					if err := addTestAnnotation(savedDash, created, sess); err != nil {
						return err
					}
				}
				return nil
			})
			So(err, ShouldBeNil)

			err = deleteExpiredAnnotations(&m.DeleteExpiredAnnotationsCommand{})
			So(err, ShouldBeNil)

			items, err := repo.Find(&annotations.ItemQuery{
				OrgId:       1,
				DashboardId: 1,
			})
			So(err, ShouldBeNil)

			// Ensure we have at least daysToKeepAnnotations versions
			So(len(items), ShouldBeGreaterThanOrEqualTo, daysToKeepAnnotations)
			// Ensure we haven't deleted more than MAX_VERSIONS_TO_DELETE rows
			So(annotationsToWriteBigNumber-len(items), ShouldBeLessThanOrEqualTo, MAX_HISTORY_ENTRIES_TO_DELETE)
		})
	})
}
