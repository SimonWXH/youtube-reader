#!/usr/bin/env python3
"""Generate the checked-in Xcode project. Run: python3 Scripts/generate_project.py"""
from pathlib import Path
import uuid

ROOT = Path(__file__).resolve().parent.parent
P = ROOT / "youtube-reader.xcodeproj" / "project.pbxproj"


def uid(seed):
    return uuid.uuid5(uuid.NAMESPACE_DNS, "youtube-reader-" + seed).hex.upper()[:24]


def f(path, last):
    return '{isa = PBXFileReference; lastKnownFileType = %s; path = "%s"; sourceTree = "<group>"; };' % (last, path)


def buildfile(file_id):
    return "{isa = PBXBuildFile; fileRef = %s /* %s */; };" % (file_id, file_id)


core = [
    ("YoutubeReader/Core/Models.swift", "sourcecode.swift"),
    ("YoutubeReader/Core/YouTubeAPI.swift", "sourcecode.swift"),
    ("YoutubeReader/Core/OPMLImporter.swift", "sourcecode.swift"),
    ("YoutubeReader/Core/SyncStore.swift", "sourcecode.swift"),
    ("YoutubeReader/Core/Primitives.swift", "sourcecode.swift"),
    ("YoutubeReader/Core/Database.swift", "sourcecode.swift"),
    ("YoutubeReader/Core/LocalSettings.swift", "sourcecode.swift"),
    ("YoutubeReader/Core/Notifier.swift", "sourcecode.swift"),
    ("YoutubeReader/Core/RefreshService.swift", "sourcecode.swift"),
    ("YoutubeReader/Core/ConnectivityMonitor.swift", "sourcecode.swift"),
]
app = [
    ("YoutubeReader/App/YouTubeReaderApp.swift", "sourcecode.swift"),
    ("YoutubeReader/App/AppModel.swift", "sourcecode.swift"),
    ("YoutubeReader/Features/ContentView.swift", "sourcecode.swift"),
    ("YoutubeReader/Features/VideoGridView.swift", "sourcecode.swift"),
    ("YoutubeReader/Features/SettingsView.swift", "sourcecode.swift"),
]
helper = [("YoutubeReaderHelper/main.swift", "sourcecode.swift")]
tests = [("Tests/YoutubeReaderXCTest/CoreTests.swift", "sourcecode.swift")]
resources = [
    ("YoutubeReader/Resources/LaunchAgent/com.simon.youtubereader.helper.plist", "text.plist.xml"),
]

# The helper lives inside the main app bundle and is copied as a LaunchAgent.
HELPER_PLIST_BUILD_FILE = uid("helperplistbuild")

lines = []
a = lines.append
a("// !$*UTF8*$!")
a("{")
a("\tarchiveVersion = 1;")
a("\tclasses = {")
a("\t};")
a("\tobjectVersion = 56;")
a("\tobjects = {")

# File references and build files
for group, items in [("core", core), ("app", app), ("helper", helper), ("tests", tests), ("resource", resources)]:
    for i, (path, ftype) in enumerate(items):
        a("\t\t%s /* %s */ = %s" % (uid(group + str(i)), path, f(path, ftype)))
    for i, (path, _) in enumerate(items):
        a("\t\t%s /* %s in Sources */ = %s" % (uid(group + "build" + str(i)), path, buildfile(uid(group + str(i)))))

# Products
a('\t\t%s /* YoutubeReaderHelper.app */ = {isa = PBXBuildFile; fileRef = %s /* YoutubeReaderHelper.app */; };' % (uid("helpercopybuild"), uid("helperproduct")))
a('\t\t%s /* com.simon.youtubereader.helper.plist */ = {isa = PBXBuildFile; fileRef = %s /* com.simon.youtubereader.helper.plist */; };' % (HELPER_PLIST_BUILD_FILE, uid("resource0")))
a('\t\t%s /* YoutubeReader.app */ = {isa = PBXFileReference; explicitFileType = wrapper.application; includeInIndex = 0; path = YoutubeReader.app; sourceTree = BUILT_PRODUCTS_DIR; };' % uid("product"))
a('\t\t%s /* YoutubeReaderHelper.app */ = {isa = PBXFileReference; explicitFileType = wrapper.application; includeInIndex = 0; path = YoutubeReaderHelper.app; sourceTree = BUILT_PRODUCTS_DIR; };' % uid("helperproduct"))
a('\t\t%s /* YoutubeReaderTests.xctest */ = {isa = PBXFileReference; explicitFileType = wrapper.cfbundle; includeInIndex = 0; path = YoutubeReaderTests.xctest; sourceTree = BUILT_PRODUCTS_DIR; };' % uid("testproduct"))

# Groups
a("\t\t%s /* Project object */ = {" % uid("project"))
a("\t\t\tisa = PBXProject;")
a("\t\t\tattributes = { BuildIndependentTargetsInParallel = 1; LastUpgradeCheck = 1500; TargetAttributes = {")
a("\t\t\t\t%s = { CreatedOnToolsVersion = 15.0; };" % uid("apptarget"))
a("\t\t\t\t%s = { CreatedOnToolsVersion = 15.0; };" % uid("helpertarget"))
a("\t\t\t\t%s = { CreatedOnToolsVersion = 15.0; TestTargetID = %s; };" % (uid("testtarget"), uid("apptarget")))
a("\t\t\t}; };")
a("\t\t\tbuildConfigurationList = %s /* Build configuration list for PBXProject */;" % uid("projectconfiglist"))
a("\t\t\tcompatibilityVersion = 'Xcode 14.0';")
a("\t\t\tdevelopmentRegion = en;")
a("\t\t\tknownRegions = ( en, Base, );")
a('\t\t\tmainGroup = %s;' % uid("maingroup"))
a('\t\t\tproductRefGroup = %s /* Products */;' % uid("productsgroup"))
a('\t\t\tprojectDirPath = "";')
a('\t\t\tprojectRoot = "";')
a("\t\t\ttargets = (%s /* YoutubeReader */, %s /* YoutubeReaderHelper */, %s /* YoutubeReaderTests */);" % (uid("apptarget"), uid("helpertarget"), uid("testtarget")))
a("\t\t};")

a("\t\t%s = { isa = PBXGroup; children = (%s /* Sources */, %s /* Helper */, %s /* Tests */, %s /* Resources */, %s /* Products */); sourceTree = '<group>'; };" % (
    uid("maingroup"), uid("sourcesgroup"), uid("helpgroup"), uid("testgroup"), uid("resourcegroup"), uid("productsgroup")))
a("\t\t%s = { isa = PBXGroup; children = (%s); path = Sources; sourceTree = '<group>'; };" % (
    uid("sourcesgroup"), ", ".join(uid("core" + str(i)) for i in range(len(core))) + ", " + ", ".join(uid("app" + str(i)) for i in range(len(app)))))
a("\t\t%s = { isa = PBXGroup; children = (%s); path = YoutubeReaderHelper; sourceTree = '<group>'; };" % (
    uid("helpgroup"), ", ".join(uid("helper" + str(i)) for i in range(len(helper)))))
a("\t\t%s = { isa = PBXGroup; children = (%s); path = Tests; sourceTree = '<group>'; };" % (
    uid("testgroup"), ", ".join(uid("tests" + str(i)) for i in range(len(tests)))))
a("\t\t%s = { isa = PBXGroup; children = (%s); path = Resources; sourceTree = '<group>'; };" % (
    uid("resourcegroup"), ", ".join(uid("resource" + str(i)) for i in range(len(resources)))))
a("\t\t%s = { isa = PBXGroup; children = (%s /* YoutubeReader.app */, %s /* YoutubeReaderHelper.app */, %s /* YoutubeReaderTests.xctest */); name = Products; sourceTree = '<group>'; };" % (
    uid("productsgroup"), uid("product"), uid("helperproduct"), uid("testproduct")))

# Native targets
def target(name, target_id, product_id, sources, product_type, bundle_id, extra_settings=""):
    a("\t\t%s /* %s */ = {" % (target_id, name))
    a("\t\t\tisa = PBXNativeTarget;")
    a("\t\t\tbuildConfigurationList = %s /* Build configuration list for %s */;" % (uid(name + "configlist"), name))
    a("\t\t\tbuildPhases = (%s /* Sources */, %s /* Frameworks */, %s /* Resources */%s);" % (uid(name + "sources"), uid(name + "frameworks"), uid(name + "resources"), ", " + uid(name + "copyfiles") if name == "YoutubeReader" else ""))
    if name == "YoutubeReader":
        a("\t\t\tdependencies = (%s /* PBXTargetDependency */);" % uid("helperdependency"))
        a("\t\t\tbuildPhases = (%s /* Sources */, %s /* Frameworks */, %s /* Resources */, %s /* Copy Files */);" % (uid(name + "sources"), uid(name + "frameworks"), uid(name + "resources"), uid(name + "copyfiles")))
    a("\t\t\tbuildRules = ();")
    if name != "YoutubeReader":
        a("\t\t\tdependencies = ();")
    a("\t\t\tname = %s;" % name)
    a("\t\t\tproductName = %s;" % name)
    a("\t\t\tproductReference = %s /* %s */;" % (product_id, name))
    a('\t\t\tproductType = "%s";' % product_type)
    a("\t\t};")
    a("\t\t%s /* Sources */ = { isa = PBXSourcesBuildPhase; files = (%s); runOnlyForDeploymentPostprocessing = 0; };" % (
        uid(name + "sources"), ", ".join(sources)))
    a("\t\t%s /* Frameworks */ = { isa = PBXFrameworksBuildPhase; files = (); runOnlyForDeploymentPostprocessing = 0; };" % uid(name + "frameworks"))
    a("\t\t%s /* Resources */ = { isa = PBXResourcesBuildPhase; files = (%s); runOnlyForDeploymentPostprocessing = 0; };" % (
        uid(name + "resources"), HELPER_PLIST_BUILD_FILE if name == "YoutubeReader" else ""))
    if name == "YoutubeReader":
        a("\t\t%s /* Copy Files */ = { isa = PBXCopyFilesBuildPhase; dstPath = Library/LaunchAgents; dstSubfolderSpec = 6; files = (%s /* YoutubeReaderHelper.app */); runOnlyForDeploymentPostprocessing = 0; };" % (
            uid(name + "copyfiles"), uid("helpercopybuild")))
        a("\t\t%s /* PBXTargetDependency */ = { isa = PBXTargetDependency; target = %s /* YoutubeReaderHelper */; targetProxy = %s /* PBXContainerItemProxy */; };" % (
            uid("helperdependency"), uid("helpertarget"), uid("helperproxy")))
        a("\t\t%s /* PBXContainerItemProxy */ = { isa = PBXContainerItemProxy; containerPortal = %s /* Project object */; proxyType = 1; remoteGlobalIDString = %s; remoteInfo = YoutubeReaderHelper; };" % (
            uid("helperproxy"), uid("project"), uid("helpertarget")))
    for cfg in ["Debug", "Release"]:
        a("\t\t%s /* %s */ = {" % (uid(name + cfg), cfg))
        a("\t\t\tisa = XCBuildConfiguration;")
        a("\t\t\tbuildSettings = {")
        a('\t\t\t\tASSETCATALOG_COMPILER_APPICON_NAME = AppIcon;')
        a('\t\t\t\tCODE_SIGN_STYLE = Automatic;')
        a('\t\t\t\tCURRENT_PROJECT_VERSION = 1;')
        a('\t\t\t\tGENERATE_INFOPLIST_FILE = YES;')
        a('\t\t\t\tINFOPLIST_KEY_LSUIElement = YES;' if name == "YoutubeReaderHelper" else '\t\t\t\tINFOPLIST_KEY_NSHumanReadableCopyright = "Private";')
        if name != "YoutubeReaderHelper":
            a('\t\t\t\tINFOPLIST_KEY_NSUserNotificationAlertStyle = alert;')
        a('\t\t\t\tIPHONEOS_DEPLOYMENT_TARGET = 13.0;')
        a('\t\t\t\tLD_RUNPATH_SEARCH_PATHS = "$(inherited) @executable_path/../Frameworks";')
        a('\t\t\t\tMACOSX_DEPLOYMENT_TARGET = 13.0;')
        a('\t\t\t\tMARKETING_VERSION = 1.0;')
        a('\t\t\t\tPRODUCT_BUNDLE_IDENTIFIER = %s;' % bundle_id)
        a('\t\t\t\tPRODUCT_NAME = "$(TARGET_NAME)";')
        a('\t\t\t\tSWIFT_EMIT_LOC_STRINGS = YES;')
        a('\t\t\t\tSWIFT_VERSION = 5.0;')
        a('\t\t\t\tSUPPORTED_PLATFORMS = macosx;')
        a('\t\t\t\tTARGETED_DEVICE_FAMILY = 1;')
        a("\t\t\t};")
        a("\t\t\tname = %s;" % cfg)
        a("\t\t};")
    a("\t\t%s = {" % uid(name + "configlist"))
    a("\t\t\tisa = XCConfigurationList;")
    a("\t\t\tbuildConfigurations = (%s /* Debug */, %s /* Release */);" % (uid(name + "Debug"), uid(name + "Release")))
    a("\t\t\tdefaultConfigurationIsVisible = 0;")
    a("\t\t\tdefaultConfigurationName = Release;")
    a("\t\t};")


app_sources = [uid("corebuild" + str(i)) for i in range(len(core))] + [uid("appbuild" + str(i)) for i in range(len(app))]
helper_sources = [uid("corebuild" + str(i)) for i in range(len(core))] + [uid("helperbuild0")]
test_sources = [uid("testsbuild0")]
target("YoutubeReader", uid("apptarget"), uid("product"), app_sources, "com.apple.product-type.application", "com.simon.youtubereader")
target("YoutubeReaderHelper", uid("helpertarget"), uid("helperproduct"), helper_sources, "com.apple.product-type.application", "com.simon.youtubereader.helper")
target("YoutubeReaderTests", uid("testtarget"), uid("testproduct"), test_sources, "com.apple.product-type.bundle.unit-test", "com.simon.youtubereader.tests")

for cfg in ["Debug", "Release"]:
    a("\t\t%s /* %s */ = {" % (uid("project" + cfg), cfg))
    a("\t\t\tisa = XCBuildConfiguration;")
    a("\t\t\tbuildSettings = {")
    a('\t\t\t\tALWAYS_SEARCH_USER_PATHS = NO;')
    a('\t\t\t\tCLANG_ENABLE_MODULES = YES;')
    a('\t\t\t\tCLANG_ENABLE_OBJC_ARC = YES;')
    a('\t\t\t\tCOPY_PHASE_STRIP = NO;')
    a('\t\t\t\tENABLE_STRICT_OBJC_MSGSEND = YES;')
    a('\t\t\t\tGCC_C_LANGUAGE_STANDARD = gnu17;')
    a('\t\t\t\tGCC_DYNAMIC_NO_PIC = NO;')
    a('\t\t\t\tGCC_NO_COMMON_BLOCKS = YES;')
    a('\t\t\t\tGCC_OPTIMIZATION_LEVEL = 0;')
    a('\t\t\t\tGCC_PREPROCESSOR_DEFINITIONS = ("DEBUG=1", "$(inherited)",);')
    a('\t\t\t\tIPHONEOS_DEPLOYMENT_TARGET = 13.0;')
    a('\t\t\t\tMACOSX_DEPLOYMENT_TARGET = 13.0;')
    a('\t\t\t\tONLY_ACTIVE_ARCH = YES;')
    a('\t\t\t\tSDKROOT = macosx;')
    a('\t\t\t\tSWIFT_ACTIVE_COMPILATION_CONDITIONS = DEBUG;')
    a('\t\t\t\tSWIFT_OPTIMIZATION_LEVEL = "-Onone";')
    a("\t\t\t};")
    a("\t\t\tname = %s;" % cfg)
    a("\t\t};")
a("\t\t%s = {" % uid("projectconfiglist"))
a("\t\t\tisa = XCConfigurationList;")
a("\t\t\tbuildConfigurations = (%s /* Debug */, %s /* Release */);" % (uid("projectDebug"), uid("projectRelease")))
a("\t\t\tdefaultConfigurationIsVisible = 0;")
a("\t\t\tdefaultConfigurationName = Release;")
a("\t\t};")
a("\t};")
a("\trootObject = %s /* Project object */;" % uid("project"))
a("}")

P.parent.mkdir(parents=True, exist_ok=True)
P.write_text("\n".join(lines) + "\n")
print(f"Wrote {P}")
