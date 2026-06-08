// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.0;

import "forge-std/Test.sol";
import "forge-std/interfaces/IERC20.sol";
import "forge-std/interfaces/IERC721.sol";

contract SimulateTxRunnerTest is Test {
  string internal constant INPUT_PATH_ENV = "TXSIM_INPUT_PATH";

  struct LabelOverride {
    address account;
    string label;
  }

  struct ERC20BalanceOverride {
    address token;
    address account;
    uint256 balance;
  }

  struct ERC20ApprovalOverride {
    address token;
    address owner;
    address spender;
    uint256 amount;
  }

  struct ERC721ApprovalOverride {
    address token;
    address owner;
    address spender;
    uint256 tokenId;
  }

  struct SimulateRequest {
    string chain;
    uint256 blockNumber;
    string projectPath;
    LabelOverride[] labelOverrides;
    ERC20BalanceOverride[] erc20BalanceOverrides;
    ERC20ApprovalOverride[] erc20ApprovalOverrides;
    ERC721ApprovalOverride[] erc721ApprovalOverrides;
    bytes stateOverrideBytecode;
    address sender;
    address target;
    bytes data;
    uint256 value;
  }

  function testSimulateTx() public {
    string memory inputPath = vm.envOr(INPUT_PATH_ENV, string(""));
    vm.skip(bytes(inputPath).length == 0, "TXSIM_INPUT_PATH is required");

    SimulateRequest memory request = _readRequest(vm.readFile(inputPath));
    _simulate(request);
  }

  function testReadRequestFromJsonFile() public view {
    SimulateRequest memory request = _readRequest(vm.readFile("test/fixtures/simulate-request.json"));

    assertEq(request.chain, "mainnet");
    assertEq(request.blockNumber, 23_000_000);
    assertEq(request.projectPath, "/tmp/project");
    assertEq(request.labelOverrides.length, 1);
    assertEq(request.labelOverrides[0].account, address(1));
    assertEq(request.labelOverrides[0].label, "Sender");
    assertEq(request.erc20BalanceOverrides.length, 1);
    assertEq(request.erc20BalanceOverrides[0].token, address(2));
    assertEq(request.erc20BalanceOverrides[0].account, address(3));
    assertEq(request.erc20BalanceOverrides[0].balance, 1000);
    assertEq(request.erc20ApprovalOverrides.length, 1);
    assertEq(request.erc20ApprovalOverrides[0].token, address(4));
    assertEq(request.erc20ApprovalOverrides[0].owner, address(5));
    assertEq(request.erc20ApprovalOverrides[0].spender, address(6));
    assertEq(request.erc20ApprovalOverrides[0].amount, 2000);
    assertEq(request.erc721ApprovalOverrides.length, 1);
    assertEq(request.erc721ApprovalOverrides[0].token, address(7));
    assertEq(request.erc721ApprovalOverrides[0].owner, address(8));
    assertEq(request.erc721ApprovalOverrides[0].spender, address(9));
    assertEq(request.erc721ApprovalOverrides[0].tokenId, 7);
    assertEq(request.stateOverrideBytecode, hex"6000");
    assertEq(request.sender, address(10));
    assertEq(request.target, address(11));
    assertEq(request.data, hex"1234");
  }

  function _simulate(SimulateRequest memory request) internal {
    for (uint256 i = 0; i < request.labelOverrides.length; i++) {
      vm.label(request.labelOverrides[i].account, request.labelOverrides[i].label);
    }

    for (uint256 i = 0; i < request.erc20BalanceOverrides.length; i++) {
      deal(
        request.erc20BalanceOverrides[i].token,
        request.erc20BalanceOverrides[i].account,
        request.erc20BalanceOverrides[i].balance
      );
    }

    for (uint256 i = 0; i < request.erc20ApprovalOverrides.length; i++) {
      vm.prank(request.erc20ApprovalOverrides[i].owner);
      IERC20(request.erc20ApprovalOverrides[i].token)
        .approve(request.erc20ApprovalOverrides[i].spender, request.erc20ApprovalOverrides[i].amount);
    }

    for (uint256 i = 0; i < request.erc721ApprovalOverrides.length; i++) {
      vm.prank(request.erc721ApprovalOverrides[i].owner);
      IERC721(request.erc721ApprovalOverrides[i].token)
        .approve(request.erc721ApprovalOverrides[i].spender, request.erc721ApprovalOverrides[i].tokenId);
    }

    if (request.stateOverrideBytecode.length > 0) {
      address stateOverridingContract;
      bytes memory stateOverrideBytecode = request.stateOverrideBytecode;
      assembly {
        stateOverridingContract := create(0, add(stateOverrideBytecode, 0x20), mload(stateOverrideBytecode))
      }

      if (stateOverridingContract == address(0)) {
        revert("Failed to deploy state overriding contract");
      }
      _callAndBubble(stateOverridingContract, "", 0);
    }

    vm.prank(request.sender);
    _callAndBubble(request.target, request.data, request.value);
  }

  function _readRequest(string memory json) internal view returns (SimulateRequest memory request) {
    request.chain = _parseOptionalString(json, ".chain");
    request.blockNumber = _parseOptionalUint(json, ".blockNumber");
    request.projectPath = _parseOptionalString(json, ".projectPath");
    request.labelOverrides = _parseLabelOverrides(json);
    request.erc20BalanceOverrides = _parseERC20BalanceOverrides(json);
    request.erc20ApprovalOverrides = _parseERC20ApprovalOverrides(json);
    request.erc721ApprovalOverrides = _parseERC721ApprovalOverrides(json);
    request.stateOverrideBytecode = _parseOptionalBytes(json, ".stateOverrideBytecode");
    request.sender = vm.parseJsonAddress(json, ".sender");
    request.target = vm.parseJsonAddress(json, ".target");
    request.data = vm.parseJsonBytes(json, ".data");
    request.value = _parseOptionalUint(json, ".value");
  }

  function _parseLabelOverrides(string memory json) internal view returns (LabelOverride[] memory) {
    uint256 length = _arrayLength(json, ".labelOverrides", "account");
    LabelOverride[] memory items = new LabelOverride[](length);
    for (uint256 i = 0; i < length; i++) {
      string memory itemKey = _arrayItemKey(".labelOverrides", i);
      items[i] = LabelOverride({
        account: vm.parseJsonAddress(json, string.concat(itemKey, ".account")),
        label: vm.parseJsonString(json, string.concat(itemKey, ".label"))
      });
    }
    return items;
  }

  function _parseERC20BalanceOverrides(string memory json) internal view returns (ERC20BalanceOverride[] memory) {
    uint256 length = _arrayLength(json, ".erc20BalanceOverrides", "token");
    ERC20BalanceOverride[] memory items = new ERC20BalanceOverride[](length);
    for (uint256 i = 0; i < length; i++) {
      string memory itemKey = _arrayItemKey(".erc20BalanceOverrides", i);
      items[i] = ERC20BalanceOverride({
        token: vm.parseJsonAddress(json, string.concat(itemKey, ".token")),
        account: vm.parseJsonAddress(json, string.concat(itemKey, ".account")),
        balance: _parseUint(json, string.concat(itemKey, ".balance"))
      });
    }
    return items;
  }

  function _parseERC20ApprovalOverrides(string memory json) internal view returns (ERC20ApprovalOverride[] memory) {
    uint256 length = _arrayLength(json, ".erc20ApprovalOverrides", "token");
    ERC20ApprovalOverride[] memory items = new ERC20ApprovalOverride[](length);
    for (uint256 i = 0; i < length; i++) {
      string memory itemKey = _arrayItemKey(".erc20ApprovalOverrides", i);
      items[i] = ERC20ApprovalOverride({
        token: vm.parseJsonAddress(json, string.concat(itemKey, ".token")),
        owner: vm.parseJsonAddress(json, string.concat(itemKey, ".owner")),
        spender: vm.parseJsonAddress(json, string.concat(itemKey, ".spender")),
        amount: _parseUint(json, string.concat(itemKey, ".amount"))
      });
    }
    return items;
  }

  function _parseERC721ApprovalOverrides(string memory json) internal view returns (ERC721ApprovalOverride[] memory) {
    uint256 length = _arrayLength(json, ".erc721ApprovalOverrides", "token");
    ERC721ApprovalOverride[] memory items = new ERC721ApprovalOverride[](length);
    for (uint256 i = 0; i < length; i++) {
      string memory itemKey = _arrayItemKey(".erc721ApprovalOverrides", i);
      items[i] = ERC721ApprovalOverride({
        token: vm.parseJsonAddress(json, string.concat(itemKey, ".token")),
        owner: vm.parseJsonAddress(json, string.concat(itemKey, ".owner")),
        spender: vm.parseJsonAddress(json, string.concat(itemKey, ".spender")),
        tokenId: _parseUint(json, string.concat(itemKey, ".tokenId"))
      });
    }
    return items;
  }

  function _parseOptionalString(string memory json, string memory key) internal view returns (string memory) {
    if (!vm.keyExistsJson(json, key)) {
      return "";
    }
    return vm.parseJsonString(json, key);
  }

  function _parseOptionalUint(string memory json, string memory key) internal view returns (uint256) {
    if (!vm.keyExistsJson(json, key)) {
      return 0;
    }
    return _parseUint(json, key);
  }

  function _parseUint(string memory json, string memory key) internal pure returns (uint256) {
    try vm.parseJsonUint(json, key) returns (uint256 value) {
      return value;
    } catch {
      return vm.parseUint(vm.parseJsonString(json, key));
    }
  }

  function _parseOptionalBytes(string memory json, string memory key) internal view returns (bytes memory) {
    if (!vm.keyExistsJson(json, key)) {
      return "";
    }
    return vm.parseJsonBytes(json, key);
  }

  function _arrayLength(string memory json, string memory key, string memory firstField)
    internal
    view
    returns (uint256 length)
  {
    while (vm.keyExistsJson(json, string.concat(_arrayItemKey(key, length), ".", firstField))) {
      length++;
    }
  }

  function _arrayItemKey(string memory key, uint256 index) internal pure returns (string memory) {
    return string.concat(key, "[", vm.toString(index), "]");
  }

  function _callAndBubble(address target, bytes memory data, uint256 value) internal {
    (bool success, bytes memory returndata) = target.call{value: value}(data);
    if (!success) {
      if (returndata.length > 0) {
        assembly {
          revert(add(returndata, 0x20), mload(returndata))
        }
      }
      revert("SimulateTxRunner: call failed");
    }
  }
}
