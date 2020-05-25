package keeperv1

import (
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/thornode/common"
	cosmos "gitlab.com/thorchain/thornode/common/cosmos"
)

type KeeperReserveContributorsSuite struct{}

var _ = Suite(&KeeperReserveContributorsSuite{})

func (KeeperReserveContributorsSuite) TestReserveContributors(c *C) {
	ctx, k := setupKeeperForTest(c)
	FundModule(c, ctx, k, AsgardName, 100000000)
	c.Assert(k.AddFeeToReserve(ctx, cosmos.NewUint(common.One*100)), IsNil)
	contributor := NewReserveContributor(GetRandomBNBAddress(), cosmos.NewUint(common.One*1000))
	contributors := ReserveContributors{
		contributor,
	}
	c.Assert(k.SetReserveContributors(ctx, contributors), IsNil)
	r, err := k.GetReservesContributors(ctx)
	c.Assert(err, IsNil)
	c.Assert(r, NotNil)
}
