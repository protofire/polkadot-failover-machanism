package azure

/*
Set
	* PREFIX
	* AZURE_SUBSCRIPTION_ID
	* AZURE_CLIENT_ID
	* AZURE_TENANT_ID
	* AZURE_RES_GROUP_NAME
	* AZURE_STORAGE_ACCOUNT
	* AZURE_STORAGE_ACCESS_KEY

before running these scripts


Additional environments:
	POLKADOT_TEST_NO_POST_TF_CLEANUP    - no terraform destroy command after tests
	POLKADOT_TEST_INITIAL_TF_CLEANUP    - terraform destroy command before tests
	POLKADOT_TEST_NO_INITIAL_TF_APPLY   - no terrafrom apply command before tests

POLKADOT_TEST_NO_POST_TF_CLEANUP=yes POLKADOT_TEST_INITIAL_TF_CLEANUP=yes make azure
POLKADOT_TEST_NO_POST_TF_CLEANUP=yes POLKADOT_TEST_NO_INITIAL_TF_APPLY=yes make azure

*/

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/protofire/polkadot-failover-mechanism/tests/azure/utils"
	"github.com/protofire/polkadot-failover-mechanism/tests/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//Gather environmental variables and set reasonable defaults
var (
	noTFApply             = len(os.Getenv("POLKADOT_TEST_NO_INITIAL_TF_APPLY")) > 0
	noPostTFCleanUp       = len(os.Getenv("POLKADOT_TEST_NO_POST_TF_CLEANUP")) > 0
	noDeleteOnTermination = len(os.Getenv("POLKADOT_TEST_NO_DELETE_ON_TERMINATION")) > 0
	forceDeleteBucket     = len(os.Getenv("POLKADOT_TEST_FORCE_DELETE_TF_BUCKET")) > 0
	azureRegions          = []string{"Central US", "East US", "West US"}
	azureSubscriptionID   = os.Getenv("AZURE_SUBSCRIPTION_ID")
	azureClientID         = os.Getenv("AZURE_CLIENT_ID")
	azureClientSecret     = os.Getenv("AZURE_CLIENT_SECRET")
	azureTenantID         = os.Getenv("AZURE_TENANT_ID")
	azureResourceGroup    = os.Getenv("AZURE_RES_GROUP_NAME")
	azureStorageAccount   = os.Getenv("AZURE_STORAGE_ACCOUNT")
	azureStorageAccessKey = os.Getenv("AZURE_STORAGE_ACCESS_KEY")
	sshUser               = "polkadot"
)

func TestBundle(t *testing.T) {

	require.NotEmpty(t, azureSubscriptionID, "AZURE_SUBSCRIPTION_ID env required")
	require.NotEmpty(t, azureClientID, "AZURE_CLIENT_ID env required")
	require.NotEmpty(t, azureClientSecret, "AZURE_CLIENT_SECRET env required")
	require.NotEmpty(t, azureTenantID, "AZURE_TENANT_ID env required")
	require.NotEmpty(t, azureResourceGroup, "AZURE_RES_GROUP_NAME env required")
	require.NotEmpty(t, azureStorageAccount, "AZURE_STORAGE_ACCOUNT env required")
	require.NotEmpty(t, azureStorageAccessKey, "AZURE_STORAGE_ACCESS_KEY env required")

	var (
		prefix         string
		azureBucket    string
		azureBucketKey string
		ok             bool
	)

	if prefix, ok = os.LookupEnv("PREFIX"); !ok || len(prefix) == 0 {
		prefix = helpers.RandStringBytes(4)
	}

	if azureBucket, ok = os.LookupEnv("TF_STATE_BUCKET"); !ok {
		azureBucket = fmt.Sprintf("%s-polkadot-validator-failover-tfstate", prefix)
	}

	if azureBucketKey, ok = os.LookupEnv("TF_STATE_KEY"); !ok {
		azureBucketKey = "terraform.tfstate"
	}

	bucketCreated, err := utils.EnsureTFBucket(azureStorageAccount, azureStorageAccessKey, azureBucket, forceDeleteBucket)
	require.NoError(t, err)
	t.Logf("TF state bucket %q has been ensured", azureBucket)

	// Generate new SSH key for test virtual machines
	sshKey := helpers.GenerateSSHKeys(t)

	// Configure Terraform - set backend, minimum set of infrastructure variables. Also expose ssh
	terraformOptions := &terraform.Options{
		// The path to where our Terraform code is located
		TerraformDir: "../../azure/",

		BackendConfig: map[string]interface{}{
			"resource_group_name":  azureResourceGroup,
			"container_name":       azureBucket,
			"key":                  azureBucketKey,
			"storage_account_name": azureStorageAccount,
			"access_key":           azureStorageAccessKey,
		},

		// Variables to pass to our Terraform code using -var options
		Vars: map[string]interface{}{
			"azure_regions":         helpers.BuildRegionParams(azureRegions...),
			"azure_client":          azureClientID,
			"azure_client_secret":   azureClientSecret,
			"azure_subscription":    azureSubscriptionID,
			"azure_tenant":          azureTenantID,
			"azure_rg":              azureResourceGroup,
			"validator_keys":        "{key1={key=\"0x6ce96ae5c300096b09dbd4567b0574f6a1281ae0e5cfe4f6b0233d1821f6206b\",type=\"gran\",seed=\"favorite liar zebra assume hurt cage any damp inherit rescue delay panic\"},key2={key=\"0x3ff0766f9ebbbceee6c2f40d9323164d07e70c70994c9d00a9512be6680c2394\",type=\"aura\",seed=\"expire stage crawl shell boss any story swamp skull yellow bamboo copy\"}}",
			"ssh_user":              sshUser,
			"ssh_key_content":       sshKey.PublicKey,
			"prefix":                prefix,
			"wait_vmss":             true,
			"use_msi":               true,
			"delete_on_termination": !noDeleteOnTermination,
			"cpu_limit":             "1",
			"ram_limit":             "1",
			"validator_name":        "test",
			"expose_ssh":            "true",
			"node_key":              "fc9c7cf9b4523759b0a43b15ff07064e70b9a2d39ef16c8f62391794469a1c5e",
			"chain":                 "westend",
			"admin_email":           "1627_DEV@altoros.com",
		},
	}

	// At the end of the test, run `terraform destroy` to clean up any resources that were created
	helpers.SetPostTFCleanUp(t, func() {
		if !noPostTFCleanUp {
			terraform.Destroy(t, terraformOptions)
			if bucketCreated {
				require.NoError(t, utils.DeleteTFBucket(azureStorageAccount, azureStorageAccessKey, azureBucket))
			} else {
				require.NoError(t, utils.ClearTFBucket(azureStorageAccount, azureStorageAccessKey, azureBucket))
			}
		} else {
			t.Log("Skipping terrafrom deferred cleanup...")
		}
	})

	if !noTFApply {
		// Run `terraform init`
		terraform.Init(t, terraformOptions)

		terraform.RunTerraformCommand(t, terraformOptions, terraform.FormatArgs(terraformOptions, "validate")...)

		helpers.SetInitialTFCleanUp(t, terraformOptions)

		// Run `terraform apply` and fail the test if there are any errors
		terraform.Apply(t, terraformOptions)
	}

	var instanceIPs []string
	vms, err := utils.GetVirtualMachineScaleSetVMs(prefix, azureSubscriptionID, azureResourceGroup)
	require.NoError(t, err)
	vmss, err := utils.GetVirtualMachineScaleSets(prefix, azureSubscriptionID, azureResourceGroup)
	require.NoError(t, err)
	regionToVMIPs, err := utils.VirtualMachineScaleSetIPAddressIDsByLocation(vmss, azureSubscriptionID, azureResourceGroup)
	require.NoError(t, err)

	for _, ips := range regionToVMIPs {
		instanceIPs = append(instanceIPs, ips...)
	}

	// TEST 1: Verify that there are healthy instances in each region with public ips assigned
	t.Run("Instances", func(t *testing.T) {

		vmsByLocation := utils.VirtualMachineScaleSetVMsByLocation(vms)
		require.Len(t, vmsByLocation, 3, "ERROR! Should be %d location. Instead: %d", 3, len(vms))

		for location, vm := range vmsByLocation {
			require.Len(t, vm, 1, "ERROR! Should be only 1 instance per location. Current localion %q has %d instances", location, len(vm))
		}

		require.NoError(t, err)
		require.Len(t, vmsByLocation, 3, "ERROR! Should be %d vm scale sets. Instead: %d", 3, len(vmss))

		for _, value := range azureRegions {
			regionInstanceIPs := regionToVMIPs[strings.ReplaceAll(strings.ToLower(value), " ", "")]
			require.GreaterOrEqualf(t, len(regionInstanceIPs), 1, "ERROR! No ip addresses found in %q region. Actual number: %d", value, len(regionInstanceIPs))
			t.Logf("INFO. The following instances found in %q region: %s.", value, strings.Join(regionInstanceIPs, ","))
		}
		t.Logf("INFO. Instances IPs found in all regions: %s", strings.Join(instanceIPs, ", "))
	})

	// TEST 2: Verify the number of existing GCP instances - should be an odd number
	t.Run("InstanceCount", func(t *testing.T) {

		instanceCount := len(instanceIPs)

		require.Equal(t, instanceCount%2, 1, "ERROR! There are even instances running")
		t.Log("INFO. There are odd instances running")

		// TEST 3: Verify the number of existing EC2 instances - should be at least 3
		require.Greaterf(t, instanceCount, 2, "ERROR! Minimum viable instance count (3) not reached. There are %d instances running.", instanceCount)
		t.Logf("INFO. Minimum viable instance count (3) reached. There are %d instances running.", instanceCount)
	})

	// TEST 4: Verify the number of Consul locks each instance is aware about. Should be exactly 1 lock on each instnace
	t.Run("ConsulVerifications", func(t *testing.T) {

		if assert.True(t, helpers.ConsulLockCheck(t, instanceIPs, sshKey, sshUser)) {
			t.Log("INFO. Consul lock check passed. Each Consul node can see exactly 1 lock.")
		}

		// TEST 5: All of the Consul nodes should be healthy
		if assert.True(t, helpers.ConsulCheck(t, instanceIPs, sshKey, sshUser)) {
			t.Log("INFO. Consul check passed. Each node can see full cluster, all nodes are healthy")
		}

	})

	t.Run("PolkadotVerifications", func(t *testing.T) {

		// TEST 6: Verify that there is only one Polkadot node working in Validator mode at a time
		if assert.True(t, helpers.LeadersCheck(t, instanceIPs, sshKey, sshUser)) {
			t.Log("INFO. Leaders check passed. Exactly 1 leader found")
		}
		// TEST 7: Verify that all Polkadot nodes are healthy
		if assert.True(t, helpers.PolkadotCheck(t, instanceIPs, sshKey, sshUser)) {
			t.Log("INFO. Polkadot node check passed. All instances are healthy")
		}

	})

	// TEST 8: All the validator keys were successfully uploaded
	t.Run("SMTests", func(t *testing.T) {

		if assert.NoError(t, utils.SMCheck(prefix, azureSubscriptionID, azureResourceGroup)) {
			t.Log("INFO. All keys were uploaded. Private key is encrypted.")
		}
	})
	// TEST 9: All the security groups were successfully created
	t.Run("FirewallTests", func(t *testing.T) {

		if assert.NoError(t, utils.SecurityGroupsCheck(prefix, azureSubscriptionID, azureResourceGroup)) {
			t.Log("INFO. All security groups were successfully created")
		}
	})

	// TEST 10: Check that all disks are being mounted
	t.Run("VolumesTests", func(t *testing.T) {

		if assert.NoError(t, utils.VolumesCheck(prefix, azureSubscriptionID, azureResourceGroup, vms)) {
			t.Log("INFO. All volumes were successfully created and attached")
		}
	})

	// TEST 11: Check that all alert policies have been created
	t.Run("AlertsTests", func(t *testing.T) {

		if assert.NoError(t, utils.AlertsCheck(prefix, azureSubscriptionID, azureResourceGroup)) {
			t.Log("INFO. All alerts rules was not fired")
		}
	})

	// TEST 12: Instances health check
	t.Run("HealthCheckTests", func(t *testing.T) {

		if assert.NoError(t, utils.HealthStatusCheck(azureSubscriptionID, azureResourceGroup, vms)) {
			t.Log("INFO. There are all healthy instances")
		}
	})

	// TEST 13: Check that there are exactly 5 keys in the keystore
	t.Run("KeystoreTests", func(t *testing.T) {

		if assert.True(t, helpers.KeystoreCheck(t, instanceIPs, sshKey, sshUser)) {
			t.Log("INFO. There are exactly 5 keys in the Keystore")
		}
	})
}
