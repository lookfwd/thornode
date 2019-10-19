package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"gitlab.com/thorchain/bepswap/common"
	. "gopkg.in/check.v1"
)

type TrustAccountSuite struct{}

var _ = Suite(&TrustAccountSuite{})

func (TrustAccountSuite) TestTrustAccount(c *C) {
	bnb, err := common.NewAddress("bnb1xlvns0n2mxh77mzaspn2hgav4rr4m8eerfju38")
	c.Assert(err, IsNil)
	addr, err := sdk.AccAddressFromBech32("bep1jtpv39zy5643vywg7a9w73ckg880lpwuqd444v")
	c.Assert(err, IsNil)
	consensusAddr := "bepcpub1zcjduepqrkasznnv37qcguhn6z33v2ndldpq00f7yldamjrtc2a0sc4vqrqqvr9t8t"
	pk, err := sdk.GetConsPubKeyBech32(consensusAddr)
	c.Assert(err, IsNil)
	c.Assert(pk, NotNil)
	c.Check(addr.Empty(), Equals, false)
	bepConsPubKey := `bepcpub1zcjduepq4kn64fcjhf0fp20gp8var0rm25ca9jy6jz7acem8gckh0nkplznq85gdrg`
	trustAccount := NewTrustAccount(bnb, addr, bepConsPubKey)
	err = trustAccount.IsValid()
	c.Assert(err, IsNil)
	c.Assert(trustAccount.ObserverBEPAddress.Equals(addr), Equals, true)
	c.Assert(trustAccount.SignerBNBAddress, Equals, bnb)
	c.Assert(trustAccount.ValidatorBEPConsPubKey, Equals, bepConsPubKey)
	c.Log(trustAccount.String())

	trustAccount1 := NewTrustAccount(common.NoAddress, addr, bepConsPubKey)
	c.Assert(trustAccount1.IsValid(), IsNil)
	c.Assert(NewTrustAccount(bnb, sdk.AccAddress{}, bepConsPubKey).IsValid(), NotNil)
	c.Assert(NewTrustAccount(bnb, addr, "").IsValid(), NotNil)
}
