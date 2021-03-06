/*
Real-time Online/Offline Charging System (OCS) for Telecom & ISP environments
Copyright (C) ITsysCOM GmbH

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package loaders

import (
	"encoding/csv"
	"errors"
	"flag"
	"io/ioutil"
	"net/rpc"
	"net/rpc/jsonrpc"
	"strings"
	"testing"

	"github.com/cgrates/rpcclient"

	"github.com/cgrates/cgrates/config"
	"github.com/cgrates/cgrates/engine"
	"github.com/cgrates/cgrates/utils"
)

var (
	waitRater = flag.Int("wait_rater", 200, "Number of miliseconds to wait for rater to start and cache")
	dataDir   = flag.String("data_dir", "/usr/share/cgrates", "CGR data dir path here")
	encoding  = flag.String("rpc", utils.MetaJSON, "what encoding whould be used for rpc comunication")
	dbType    = flag.String("dbtype", utils.MetaInternal, "The type of DataBase (Internal/Mongo/mySql)")
)

var loaderPaths = []string{"/tmp/In", "/tmp/Out", "/tmp/LoaderIn", "/tmp/SubpathWithoutMove",
	"/tmp/SubpathLoaderWithMove", "/tmp/SubpathOut", "/tmp/templateLoaderIn", "/tmp/templateLoaderOut",
	"/tmp/customSepLoaderIn", "/tmp/customSepLoaderOut"}

func newRPCClient(cfg *config.ListenCfg) (c *rpc.Client, err error) {
	switch *encoding {
	case utils.MetaJSON:
		return jsonrpc.Dial(utils.TCP, cfg.RPCJSONListen)
	case utils.MetaGOB:
		return rpc.Dial(utils.TCP, cfg.RPCGOBListen)
	default:
		return nil, errors.New("UNSUPPORTED_RPC")
	}
}

type testMockCacheConn struct {
	calls map[string]func(arg interface{}, rply interface{}) error
}

func (s *testMockCacheConn) Call(method string, arg interface{}, rply interface{}) error {
	if call, has := s.calls[method]; !has {
		return rpcclient.ErrUnsupporteServiceMethod
	} else {
		return call(arg, rply)
	}
}

func TestProcessContentCallsLoadCache(t *testing.T) {
	sMock := &testMockCacheConn{
		calls: map[string]func(arg interface{}, rply interface{}) error{
			utils.CacheSv1LoadCache: func(arg interface{}, rply interface{}) error {
				prply, can := rply.(*string)
				if !can {
					t.Errorf("Wrong argument type: %T", rply)
				}
				*prply = utils.OK
				return nil
			},
			utils.CacheSv1Clear: func(arg interface{}, rply interface{}) error {
				prply, can := rply.(*string)
				if !can {
					t.Errorf("Wrong argument type : %T", rply)
					return nil
				}
				*prply = utils.OK
				return nil
			},
		},
	}
	internalCacheSChann := make(chan rpcclient.ClientConnector, 1)
	internalCacheSChann <- sMock
	data := engine.NewInternalDB(nil, nil, true)
	ldr := &Loader{
		ldrID:         "TestProcessContentCallsLoadCache",
		bufLoaderData: make(map[string][]LoaderData),
		dm:            engine.NewDataManager(data, config.CgrConfig().CacheCfg(), nil),
		connMgr: engine.NewConnManager(config.CgrConfig(), map[string]chan rpcclient.ClientConnector{
			utils.ConcatenatedKey(utils.MetaInternal, utils.MetaCaches): internalCacheSChann,
		}),
		cacheConns: []string{utils.ConcatenatedKey(utils.MetaInternal, utils.MetaCaches)},
		timezone:   "UTC",
	}
	ldr.dataTpls = map[string][]*config.FCTemplate{
		utils.MetaRateProfiles: {
			{Tag: "TenantID",
				Path:      "Tenant",
				Type:      utils.MetaComposed,
				Value:     config.NewRSRParsersMustCompile("~*req.0", utils.InfieldSep),
				Mandatory: true},
			{Tag: "ProfileID",
				Path:      "ID",
				Type:      utils.MetaComposed,
				Value:     config.NewRSRParsersMustCompile("~*req.1", utils.InfieldSep),
				Mandatory: true},
			{Tag: "Weight",
				Path:  "Weight",
				Type:  utils.MetaComposed,
				Value: config.NewRSRParsersMustCompile("~*req.2", utils.InfieldSep)},
		},
	}
	ratePrfCsv := `
#Tenant[0],ID[1],Weight[2]
cgrates.org,MOCK_RELOAD_ID,20
`
	rdr := ioutil.NopCloser(strings.NewReader(ratePrfCsv))
	rdrCsv := csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaRateProfiles: {
			utils.RateProfilesCsv: &openedCSVFile{
				fileName: utils.RateProfilesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}
	if err := ldr.processContent(utils.MetaRateProfiles, utils.MetaLoad); err != nil {
		t.Error(err)
	}

	// Calling the method again while cacheConnsID is not valid
	ldr.cacheConns = []string{utils.MetaInternal}
	rdr = ioutil.NopCloser(strings.NewReader(ratePrfCsv))
	rdrCsv = csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaRateProfiles: {
			utils.RateProfilesCsv: &openedCSVFile{
				fileName: utils.RateProfilesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}
	expected := "UNSUPPORTED_SERVICE_METHOD"
	if err := ldr.processContent(utils.MetaRateProfiles, utils.MetaLoad); err == nil || err.Error() != expected {
		t.Errorf("Expected %+v, received %+v", expected, err)
	}
}

func TestProcessContentCallsReloadCache(t *testing.T) {
	// Clear cache because connManager sets the internal connection in cache
	engine.Cache.Clear([]string{utils.CacheRPCConnections})

	sMock2 := &testMockCacheConn{
		calls: map[string]func(arg interface{}, rply interface{}) error{
			utils.CacheSv1ReloadCache: func(arg interface{}, rply interface{}) error {
				prply, can := rply.(*string)
				if !can {
					t.Errorf("Wrong argument type : %T", rply)
					return nil
				}
				*prply = utils.OK
				return nil
			},
			utils.CacheSv1Clear: func(arg interface{}, rply interface{}) error {
				prply, can := rply.(*string)
				if !can {
					t.Errorf("Wrong argument type : %T", rply)
					return nil
				}
				*prply = utils.OK
				return nil
			},
		},
	}
	data := engine.NewInternalDB(nil, nil, true)

	internalCacheSChan := make(chan rpcclient.ClientConnector, 1)
	internalCacheSChan <- sMock2
	ldr := &Loader{
		ldrID:         "TestProcessContentCalls",
		bufLoaderData: make(map[string][]LoaderData),
		connMgr: engine.NewConnManager(config.CgrConfig(), map[string]chan rpcclient.ClientConnector{
			utils.ConcatenatedKey(utils.MetaInternal, utils.MetaCaches): internalCacheSChan,
		}),
		dm:         engine.NewDataManager(data, config.CgrConfig().CacheCfg(), nil),
		cacheConns: []string{utils.ConcatenatedKey(utils.MetaInternal, utils.MetaCaches)},
		timezone:   "UTC",
	}
	ldr.dataTpls = map[string][]*config.FCTemplate{
		utils.MetaRateProfiles: {
			{Tag: "TenantID",
				Path:      "Tenant",
				Type:      utils.MetaComposed,
				Value:     config.NewRSRParsersMustCompile("~*req.0", utils.InfieldSep),
				Mandatory: true},
			{Tag: "ProfileID",
				Path:      "ID",
				Type:      utils.MetaComposed,
				Value:     config.NewRSRParsersMustCompile("~*req.1", utils.InfieldSep),
				Mandatory: true},
			{Tag: "Weight",
				Path:  "Weight",
				Type:  utils.MetaComposed,
				Value: config.NewRSRParsersMustCompile("~*req.2", utils.InfieldSep)},
		},
	}
	ratePrfCsv := `
#Tenant[0],ID[1],Weight[2]
cgrates.org,MOCK_RELOAD_ID,20
`
	rdr := ioutil.NopCloser(strings.NewReader(ratePrfCsv))
	rdrCsv := csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaRateProfiles: {
			utils.RateProfilesCsv: &openedCSVFile{
				fileName: utils.RateProfilesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}
	if err := ldr.processContent(utils.MetaRateProfiles, utils.MetaReload); err != nil {
		t.Error(err)
	}

	// Calling the method again while cacheConnsID is not valid
	ldr.cacheConns = []string{utils.MetaInternal}
	rdr = ioutil.NopCloser(strings.NewReader(ratePrfCsv))
	rdrCsv = csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaRateProfiles: {
			utils.RateProfilesCsv: &openedCSVFile{
				fileName: utils.RateProfilesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}
	expected := "UNSUPPORTED_SERVICE_METHOD"
	if err := ldr.processContent(utils.MetaRateProfiles, utils.MetaReload); err == nil || err.Error() != expected {
		t.Errorf("Expected %+v, received %+v", expected, err)
	}
}

func TestProcessContentCallsRemoveItems(t *testing.T) {
	// Clear cache because connManager sets the internal connection in cache
	engine.Cache.Clear([]string{utils.CacheRPCConnections})

	sMock := &testMockCacheConn{
		calls: map[string]func(arg interface{}, rply interface{}) error{
			utils.CacheSv1RemoveItems: func(arg interface{}, rply interface{}) error {
				prply, can := rply.(*string)
				if !can {
					t.Errorf("Wrong argument type : %T", rply)
					return nil
				}
				*prply = utils.OK
				return nil
			},
			utils.CacheSv1Clear: func(arg interface{}, rply interface{}) error {
				prply, can := rply.(*string)
				if !can {
					t.Errorf("Wrong argument type : %T", rply)
					return nil
				}
				*prply = utils.OK
				return nil
			},
		},
	}
	data := engine.NewInternalDB(nil, nil, true)

	internalCacheSChan := make(chan rpcclient.ClientConnector, 1)
	internalCacheSChan <- sMock
	ldr := &Loader{
		ldrID:         "TestProcessContentCallsRemoveItems",
		bufLoaderData: make(map[string][]LoaderData),
		connMgr: engine.NewConnManager(config.CgrConfig(), map[string]chan rpcclient.ClientConnector{
			utils.ConcatenatedKey(utils.MetaInternal, utils.MetaCaches): internalCacheSChan,
		}),
		dm:         engine.NewDataManager(data, config.CgrConfig().CacheCfg(), nil),
		cacheConns: []string{utils.ConcatenatedKey(utils.MetaInternal, utils.MetaCaches)},
		timezone:   "UTC",
	}
	ldr.dataTpls = map[string][]*config.FCTemplate{
		utils.MetaAttributes: {
			{Tag: "TenantID",
				Path:      "Tenant",
				Type:      utils.MetaComposed,
				Value:     config.NewRSRParsersMustCompile("~*req.0", utils.InfieldSep),
				Mandatory: true},
			{Tag: "ProfileID",
				Path:      "ID",
				Type:      utils.MetaComposed,
				Value:     config.NewRSRParsersMustCompile("~*req.1", utils.InfieldSep),
				Mandatory: true},
		},
	}
	attributeCsv := `
#Tenant[0],ID[1]
cgrates.org,MOCK_RELOAD_ID
`
	rdr := ioutil.NopCloser(strings.NewReader(attributeCsv))
	rdrCsv := csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaAttributes: {
			utils.AttributesCsv: &openedCSVFile{
				fileName: utils.AttributesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}
	if err := ldr.processContent(utils.MetaAttributes, utils.MetaRemove); err != nil {
		t.Error(err)
	}

	// Calling the method again while cacheConnsID is not valid
	ldr.cacheConns = []string{utils.MetaInternal}
	rdr = ioutil.NopCloser(strings.NewReader(attributeCsv))
	rdrCsv = csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaAttributes: {
			utils.AttributesCsv: &openedCSVFile{
				fileName: utils.AttributesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}
	expected := "UNSUPPORTED_SERVICE_METHOD"
	if err := ldr.processContent(utils.MetaAttributes, utils.MetaRemove); err == nil || err.Error() != expected {
		t.Errorf("Expected %+v, received %+v", expected, err)
	}

	// Calling the method again while caching method is invalid
	ldr.cacheConns = []string{utils.MetaInternal}
	rdr = ioutil.NopCloser(strings.NewReader(attributeCsv))
	rdrCsv = csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaAttributes: {
			utils.AttributesCsv: &openedCSVFile{
				fileName: utils.AttributesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}
	expected = "UNSUPPORTED_SERVICE_METHOD"
	if err := ldr.processContent(utils.MetaAttributes, "invalid_caching_api"); err == nil || err.Error() != expected {
		t.Errorf("Expected %+v, received %+v", expected, err)
	}
}

func TestProcessContentCallsClear(t *testing.T) {
	// Clear cache because connManager sets the internal connection in cache
	engine.Cache.Clear([]string{utils.CacheRPCConnections})

	sMock := &testMockCacheConn{
		calls: map[string]func(arg interface{}, rply interface{}) error{
			utils.CacheSv1Clear: func(arg interface{}, rply interface{}) error {
				prply, can := rply.(*string)
				if !can {
					t.Errorf("Wrong argument type : %T", rply)
					return nil
				}
				*prply = utils.OK
				return nil
			},
		},
	}
	data := engine.NewInternalDB(nil, nil, true)

	internalCacheSChan := make(chan rpcclient.ClientConnector, 1)
	internalCacheSChan <- sMock
	ldr := &Loader{
		ldrID:         "TestProcessContentCallsClear",
		bufLoaderData: make(map[string][]LoaderData),
		connMgr: engine.NewConnManager(config.CgrConfig(), map[string]chan rpcclient.ClientConnector{
			utils.ConcatenatedKey(utils.MetaInternal, utils.MetaCaches): internalCacheSChan,
		}),
		dm:         engine.NewDataManager(data, config.CgrConfig().CacheCfg(), nil),
		cacheConns: []string{utils.ConcatenatedKey(utils.MetaInternal, utils.MetaCaches)},
		timezone:   "UTC",
	}
	ldr.dataTpls = map[string][]*config.FCTemplate{
		utils.MetaAttributes: {
			{Tag: "TenantID",
				Path:      "Tenant",
				Type:      utils.MetaComposed,
				Value:     config.NewRSRParsersMustCompile("~*req.0", utils.InfieldSep),
				Mandatory: true},
			{Tag: "ProfileID",
				Path:      "ID",
				Type:      utils.MetaComposed,
				Value:     config.NewRSRParsersMustCompile("~*req.1", utils.InfieldSep),
				Mandatory: true},
		},
	}
	attributeCsv := `
#Tenant[0],ID[1]
cgrates.org,MOCK_RELOAD_ID
`
	rdr := ioutil.NopCloser(strings.NewReader(attributeCsv))
	rdrCsv := csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaAttributes: {
			utils.AttributesCsv: &openedCSVFile{
				fileName: utils.AttributesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}
	if err := ldr.processContent(utils.MetaAttributes, utils.MetaClear); err != nil {
		t.Error(err)
	}

	//inexisting method(*none) of cache and reinitialized the reader will do nothing
	rdr = ioutil.NopCloser(strings.NewReader(attributeCsv))
	rdrCsv = csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaAttributes: {
			utils.AttributesCsv: &openedCSVFile{
				fileName: utils.AttributesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}
	if err := ldr.processContent(utils.MetaAttributes, utils.MetaNone); err != nil {
		t.Error(err)
	}

	// Calling the method again while cacheConnsID is not valid
	ldr.cacheConns = []string{utils.MetaInternal}
	rdr = ioutil.NopCloser(strings.NewReader(attributeCsv))
	rdrCsv = csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaAttributes: {
			utils.AttributesCsv: &openedCSVFile{
				fileName: utils.AttributesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}
	expected := "UNSUPPORTED_SERVICE_METHOD"
	if err := ldr.processContent(utils.MetaAttributes, utils.MetaClear); err == nil || err.Error() != expected {
		t.Errorf("Expected %+v, received %+v", expected, err)
	}
}

func TestRemoveContentCallsReload(t *testing.T) {
	// Clear cache because connManager sets the internal connection in cache
	engine.Cache.Clear([]string{utils.CacheRPCConnections})

	sMock := &testMockCacheConn{
		calls: map[string]func(arg interface{}, rply interface{}) error{
			utils.CacheSv1ReloadCache: func(arg interface{}, rply interface{}) error {
				prply, can := rply.(*string)
				if !can {
					t.Errorf("Wrong argument type : %T", rply)
					return nil
				}
				*prply = utils.OK
				return nil
			},
			utils.CacheSv1Clear: func(arg interface{}, rply interface{}) error {
				prply, can := rply.(*string)
				if !can {
					t.Errorf("Wrong argument type : %T", rply)
					return nil
				}
				*prply = utils.OK
				return nil
			},
		},
	}
	data := engine.NewInternalDB(nil, nil, true)

	internalCacheSChan := make(chan rpcclient.ClientConnector, 1)
	internalCacheSChan <- sMock
	ldr := &Loader{
		ldrID:         "TestRemoveContentCallsReload",
		bufLoaderData: make(map[string][]LoaderData),
		connMgr: engine.NewConnManager(config.CgrConfig(), map[string]chan rpcclient.ClientConnector{
			utils.ConcatenatedKey(utils.MetaInternal, utils.MetaCaches): internalCacheSChan,
		}),
		cacheConns: []string{utils.ConcatenatedKey(utils.MetaInternal, utils.MetaCaches)},
		dm:         engine.NewDataManager(data, config.CgrConfig().CacheCfg(), nil),
		timezone:   "UTC",
	}
	ldr.dataTpls = map[string][]*config.FCTemplate{
		utils.MetaAttributes: {
			{Tag: "TenantID",
				Path:      "Tenant",
				Type:      utils.MetaComposed,
				Value:     config.NewRSRParsersMustCompile("~*req.0", utils.InfieldSep),
				Mandatory: true},
			{Tag: "ProfileID",
				Path:      "ID",
				Type:      utils.MetaComposed,
				Value:     config.NewRSRParsersMustCompile("~*req.1", utils.InfieldSep),
				Mandatory: true},
		},
	}
	attributeCsv := `
#Tenant[0],ID[1]
cgrates.org,MOCK_RELOAD_2
`
	rdr := ioutil.NopCloser(strings.NewReader(attributeCsv))
	rdrCsv := csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaAttributes: {
			utils.AttributesCsv: &openedCSVFile{
				fileName: utils.AttributesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}
	attrPrf := &engine.AttributeProfile{
		Tenant: "cgrates.org",
		ID:     "MOCK_RELOAD_2",
	}
	if err := ldr.dm.SetAttributeProfile(attrPrf, true); err != nil {
		t.Error(err)
	}
	if err := ldr.removeContent(utils.MetaAttributes, utils.MetaReload); err != nil {
		t.Error(err)
	}

	//Calling the method again while cacheConnsID is not valid
	ldr.cacheConns = []string{utils.MetaInternal}
	rdr = ioutil.NopCloser(strings.NewReader(attributeCsv))
	rdrCsv = csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaAttributes: {
			utils.AttributesCsv: &openedCSVFile{
				fileName: utils.AttributesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}

	//set and remove again from database
	if err := ldr.dm.SetAttributeProfile(attrPrf, true); err != nil {
		t.Error(err)
	}
	expected := "UNSUPPORTED_SERVICE_METHOD"
	if err := ldr.removeContent(utils.MetaAttributes, utils.MetaReload); err == nil || err.Error() != expected {
		t.Errorf("Expected %+v, received %+v", expected, err)
	}
}

func TestRemoveContentCallsLoad(t *testing.T) {
	// Clear cache because connManager sets the internal connection in cache
	engine.Cache.Clear([]string{utils.CacheRPCConnections})

	sMock := &testMockCacheConn{
		calls: map[string]func(arg interface{}, rply interface{}) error{
			utils.CacheSv1LoadCache: func(arg interface{}, rply interface{}) error {
				prply, can := rply.(*string)
				if !can {
					t.Errorf("Wrong argument type : %T", rply)
					return nil
				}
				*prply = utils.OK
				return nil
			},
			utils.CacheSv1Clear: func(arg interface{}, rply interface{}) error {
				prply, can := rply.(*string)
				if !can {
					t.Errorf("Wrong argument type : %T", rply)
					return nil
				}
				*prply = utils.OK
				return nil
			},
		},
	}
	data := engine.NewInternalDB(nil, nil, true)

	internalCacheSChan := make(chan rpcclient.ClientConnector, 1)
	internalCacheSChan <- sMock
	ldr := &Loader{
		ldrID:         "TestRemoveContentCallsReload",
		bufLoaderData: make(map[string][]LoaderData),
		connMgr: engine.NewConnManager(config.CgrConfig(), map[string]chan rpcclient.ClientConnector{
			utils.ConcatenatedKey(utils.MetaInternal, utils.MetaCaches): internalCacheSChan,
		}),
		cacheConns: []string{utils.ConcatenatedKey(utils.MetaInternal, utils.MetaCaches)},
		dm:         engine.NewDataManager(data, config.CgrConfig().CacheCfg(), nil),
		timezone:   "UTC",
	}
	ldr.dataTpls = map[string][]*config.FCTemplate{
		utils.MetaAttributes: {
			{Tag: "TenantID",
				Path:      "Tenant",
				Type:      utils.MetaComposed,
				Value:     config.NewRSRParsersMustCompile("~*req.0", utils.InfieldSep),
				Mandatory: true},
			{Tag: "ProfileID",
				Path:      "ID",
				Type:      utils.MetaComposed,
				Value:     config.NewRSRParsersMustCompile("~*req.1", utils.InfieldSep),
				Mandatory: true},
		},
	}
	attributeCsv := `
#Tenant[0],ID[1]
cgrates.org,MOCK_RELOAD_3
`
	rdr := ioutil.NopCloser(strings.NewReader(attributeCsv))
	rdrCsv := csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaAttributes: {
			utils.AttributesCsv: &openedCSVFile{
				fileName: utils.AttributesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}
	attrPrf := &engine.AttributeProfile{
		Tenant: "cgrates.org",
		ID:     "MOCK_RELOAD_3",
	}
	if err := ldr.dm.SetAttributeProfile(attrPrf, true); err != nil {
		t.Error(err)
	}
	if err := ldr.removeContent(utils.MetaAttributes, utils.MetaLoad); err != nil {
		t.Error(err)
	}

	//Calling the method again while cacheConnsID is not valid
	ldr.cacheConns = []string{utils.MetaInternal}
	rdr = ioutil.NopCloser(strings.NewReader(attributeCsv))
	rdrCsv = csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaAttributes: {
			utils.AttributesCsv: &openedCSVFile{
				fileName: utils.AttributesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}

	//set and remove again from database
	if err := ldr.dm.SetAttributeProfile(attrPrf, true); err != nil {
		t.Error(err)
	}
	expected := "UNSUPPORTED_SERVICE_METHOD"
	if err := ldr.removeContent(utils.MetaAttributes, utils.MetaLoad); err == nil || err.Error() != expected {
		t.Errorf("Expected %+v, received %+v", expected, err)
	}
}

func TestRemoveContentCallsRemove(t *testing.T) {
	// Clear cache because connManager sets the internal connection in cache
	engine.Cache.Clear([]string{utils.CacheRPCConnections})

	sMock := &testMockCacheConn{
		calls: map[string]func(arg interface{}, rply interface{}) error{
			utils.CacheSv1RemoveItems: func(arg interface{}, rply interface{}) error {
				prply, can := rply.(*string)
				if !can {
					t.Errorf("Wrong argument type : %T", rply)
					return nil
				}
				*prply = utils.OK
				return nil
			},
			utils.CacheSv1Clear: func(arg interface{}, rply interface{}) error {
				prply, can := rply.(*string)
				if !can {
					t.Errorf("Wrong argument type : %T", rply)
					return nil
				}
				*prply = utils.OK
				return nil
			},
		},
	}
	data := engine.NewInternalDB(nil, nil, true)

	internalCacheSChan := make(chan rpcclient.ClientConnector, 1)
	internalCacheSChan <- sMock
	ldr := &Loader{
		ldrID:         "TestRemoveContentCallsReload",
		bufLoaderData: make(map[string][]LoaderData),
		connMgr: engine.NewConnManager(config.CgrConfig(), map[string]chan rpcclient.ClientConnector{
			utils.ConcatenatedKey(utils.MetaInternal, utils.MetaCaches): internalCacheSChan,
		}),
		cacheConns: []string{utils.ConcatenatedKey(utils.MetaInternal, utils.MetaCaches)},
		dm:         engine.NewDataManager(data, config.CgrConfig().CacheCfg(), nil),
		timezone:   "UTC",
	}
	ldr.dataTpls = map[string][]*config.FCTemplate{
		utils.MetaAttributes: {
			{Tag: "TenantID",
				Path:      "Tenant",
				Type:      utils.MetaComposed,
				Value:     config.NewRSRParsersMustCompile("~*req.0", utils.InfieldSep),
				Mandatory: true},
			{Tag: "ProfileID",
				Path:      "ID",
				Type:      utils.MetaComposed,
				Value:     config.NewRSRParsersMustCompile("~*req.1", utils.InfieldSep),
				Mandatory: true},
		},
	}
	attributeCsv := `
#Tenant[0],ID[1]
cgrates.org,MOCK_RELOAD_4
`
	rdr := ioutil.NopCloser(strings.NewReader(attributeCsv))
	rdrCsv := csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaAttributes: {
			utils.AttributesCsv: &openedCSVFile{
				fileName: utils.AttributesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}
	attrPrf := &engine.AttributeProfile{
		Tenant: "cgrates.org",
		ID:     "MOCK_RELOAD_4",
	}
	if err := ldr.dm.SetAttributeProfile(attrPrf, true); err != nil {
		t.Error(err)
	}
	if err := ldr.removeContent(utils.MetaAttributes, utils.MetaRemove); err != nil {
		t.Error(err)
	}

	//Calling the method again while cacheConnsID is not valid
	ldr.cacheConns = []string{utils.MetaInternal}
	rdr = ioutil.NopCloser(strings.NewReader(attributeCsv))
	rdrCsv = csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaAttributes: {
			utils.AttributesCsv: &openedCSVFile{
				fileName: utils.AttributesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}

	//set and remove again from database
	if err := ldr.dm.SetAttributeProfile(attrPrf, true); err != nil {
		t.Error(err)
	}
	expected := "UNSUPPORTED_SERVICE_METHOD"
	if err := ldr.removeContent(utils.MetaAttributes, utils.MetaRemove); err == nil || err.Error() != expected {
		t.Errorf("Expected %+v, received %+v", expected, err)
	}

	//inexisting method(*none) of cache and reinitialized the reader will do nothing
	rdr = ioutil.NopCloser(strings.NewReader(attributeCsv))
	rdrCsv = csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaAttributes: {
			utils.AttributesCsv: &openedCSVFile{
				fileName: utils.AttributesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}
	if err := ldr.dm.SetAttributeProfile(attrPrf, true); err != nil {
		t.Error(err)
	}
	if err := ldr.removeContent(utils.MetaAttributes, utils.MetaNone); err != nil {
		t.Error(err)
	}
}

func TestRemoveContentCallsClear(t *testing.T) {
	// Clear cache because connManager sets the internal connection in cache
	engine.Cache.Clear([]string{utils.CacheRPCConnections})

	sMock := &testMockCacheConn{
		calls: map[string]func(arg interface{}, rply interface{}) error{
			utils.CacheSv1Clear: func(arg interface{}, rply interface{}) error {
				prply, can := rply.(*string)
				if !can {
					t.Errorf("Wrong argument type : %T", rply)
					return nil
				}
				*prply = utils.OK
				return nil
			},
		},
	}
	data := engine.NewInternalDB(nil, nil, true)

	internalCacheSChan := make(chan rpcclient.ClientConnector, 1)
	internalCacheSChan <- sMock
	ldr := &Loader{
		ldrID:         "TestRemoveContentCallsReload",
		bufLoaderData: make(map[string][]LoaderData),
		connMgr: engine.NewConnManager(config.CgrConfig(), map[string]chan rpcclient.ClientConnector{
			utils.ConcatenatedKey(utils.MetaInternal, utils.MetaCaches): internalCacheSChan,
		}),
		cacheConns: []string{utils.ConcatenatedKey(utils.MetaInternal, utils.MetaCaches)},
		dm:         engine.NewDataManager(data, config.CgrConfig().CacheCfg(), nil),
		timezone:   "UTC",
	}
	ldr.dataTpls = map[string][]*config.FCTemplate{
		utils.MetaAttributes: {
			{Tag: "TenantID",
				Path:      "Tenant",
				Type:      utils.MetaComposed,
				Value:     config.NewRSRParsersMustCompile("~*req.0", utils.InfieldSep),
				Mandatory: true},
			{Tag: "ProfileID",
				Path:      "ID",
				Type:      utils.MetaComposed,
				Value:     config.NewRSRParsersMustCompile("~*req.1", utils.InfieldSep),
				Mandatory: true},
		},
	}
	attributeCsv := `
#Tenant[0],ID[1]
cgrates.org,MOCK_RELOAD_3
`
	rdr := ioutil.NopCloser(strings.NewReader(attributeCsv))
	rdrCsv := csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaAttributes: {
			utils.AttributesCsv: &openedCSVFile{
				fileName: utils.AttributesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}
	attrPrf := &engine.AttributeProfile{
		Tenant: "cgrates.org",
		ID:     "MOCK_RELOAD_3",
	}
	if err := ldr.dm.SetAttributeProfile(attrPrf, true); err != nil {
		t.Error(err)
	}
	if err := ldr.removeContent(utils.MetaAttributes, utils.MetaClear); err != nil {
		t.Error(err)
	}

	//Calling the method again while cacheConnsID is not valid
	ldr.cacheConns = []string{utils.MetaInternal}
	rdr = ioutil.NopCloser(strings.NewReader(attributeCsv))
	rdrCsv = csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaAttributes: {
			utils.AttributesCsv: &openedCSVFile{
				fileName: utils.AttributesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}

	//set and remove again from database
	if err := ldr.dm.SetAttributeProfile(attrPrf, true); err != nil {
		t.Error(err)
	}
	expected := "UNSUPPORTED_SERVICE_METHOD"
	if err := ldr.removeContent(utils.MetaAttributes, utils.MetaClear); err == nil || err.Error() != expected {
		t.Errorf("Expected %+v, received %+v", expected, err)
	}

	// Calling the method again while caching method is invalid
	rdr = ioutil.NopCloser(strings.NewReader(attributeCsv))
	rdrCsv = csv.NewReader(rdr)
	rdrCsv.Comment = '#'
	ldr.rdrs = map[string]map[string]*openedCSVFile{
		utils.MetaAttributes: {
			utils.AttributesCsv: &openedCSVFile{
				fileName: utils.AttributesCsv,
				rdr:      rdr,
				csvRdr:   rdrCsv,
			},
		},
	}
	if err := ldr.dm.SetAttributeProfile(attrPrf, true); err != nil {
		t.Error(err)
	}
	expected = "UNSUPPORTED_SERVICE_METHOD"
	if err := ldr.removeContent(utils.MetaAttributes, "invalid_caching_api"); err == nil || err.Error() != expected {
		t.Errorf("Expected %+v, received %+v", expected, err)
	}
}

//mocking in order to fail RemoveThreshold method for coverage
type dataDBMockError struct {
	*engine.DataDBMock
}

//For Threshold
func (dbM *dataDBMockError) RemThresholdProfileDrv(tenant, id string) (err error) {
	return
}

func (dbM *dataDBMockError) SetIndexesDrv(idxItmType, tntCtx string,
	indexes map[string]utils.StringSet, commit bool, transactionID string) (err error) {
	return
}

func (dbM *dataDBMockError) RemoveThresholdDrv(string, string) error {
	return utils.ErrNoDatabaseConn
}

func (dbM *dataDBMockError) GetThresholdProfileDrv(tenant string, ID string) (tp *engine.ThresholdProfile, err error) {
	expThresholdPrf := &engine.ThresholdProfile{
		Tenant: "cgrates.org",
		ID:     "REM_THRESHOLDS_1",
	}
	return expThresholdPrf, nil
}

func (dbM *dataDBMockError) SetThresholdProfileDrv(tp *engine.ThresholdProfile) (err error) {
	return
}

func (dbM *dataDBMockError) GetThresholdDrv(string, string) (*engine.Threshold, error) {
	return nil, utils.ErrNoDatabaseConn
}

func (dbM *dataDBMockError) HasDataDrv(string, string, string) (bool, error) {
	return false, nil
}

//For StatQueue
func (dbM *dataDBMockError) GetStatQueueProfileDrv(tenant string, ID string) (sq *engine.StatQueueProfile, err error) {
	return nil, nil
}

func (dbM *dataDBMockError) RemStatQueueProfileDrv(tenant, id string) (err error) {
	return nil
}

func (dbM *dataDBMockError) RemStatQueueDrv(tenant, id string) (err error) {
	return utils.ErrNoDatabaseConn
}

func (dbM *dataDBMockError) GetStatQueueDrv(tenant, id string) (sq *engine.StatQueue, err error) {
	return nil, utils.ErrNoDatabaseConn
}

func (dbM *dataDBMockError) SetStatQueueDrv(ssq *engine.StoredStatQueue, sq *engine.StatQueue) (err error) {
	return utils.ErrNoDatabaseConn
}

func (dbM *dataDBMockError) SetStatQueueProfileDrv(sq *engine.StatQueueProfile) (err error) {
	return nil
}

//For Resources
func (dbM *dataDBMockError) GetResourceProfileDrv(string, string) (*engine.ResourceProfile, error) {
	return nil, nil
}

func (dbM *dataDBMockError) RemoveResourceProfileDrv(string, string) error {
	return nil
}

func (dbM *dataDBMockError) RemoveResourceDrv(tenant, id string) (err error) {
	return utils.ErrNoDatabaseConn
}

func (dbM *dataDBMockError) GetIndexesDrv(idxItmType, tntCtx, idxKey string) (indexes map[string]utils.StringSet, err error) {
	return nil, nil
}

func (dbM *dataDBMockError) SetResourceProfileDrv(*engine.ResourceProfile) error {
	return nil
}

func (dbM *dataDBMockError) SetResourceDrv(*engine.Resource) error {
	return utils.ErrNoDatabaseConn
}
