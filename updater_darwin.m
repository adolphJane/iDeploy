#import <Foundation/Foundation.h>
#import <dispatch/dispatch.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// Keep the bridge independent of Sparkle headers so normal Go builds do not
// need the release framework installed on the development machine.
@interface IDSparkleUpdater : NSObject
- (BOOL)automaticallyChecksForUpdates;
- (void)setAutomaticallyChecksForUpdates:(BOOL)enabled;
- (void)checkForUpdatesInBackground;
@end

@interface IDSparkleController : NSObject
- (instancetype)initWithStartingUpdater:(BOOL)start
                         updaterDelegate:(id)delegate
                       userDriverDelegate:(id)driver;
- (IDSparkleUpdater *)updater;
- (void)checkForUpdates:(id)sender;
@end

static char ideployUpdatePhase[32] = "idle";
static char ideployUpdateDetail[512] = "";

static void ideploy_set_update_progress(const char *phase, NSString *detail) {
    snprintf(ideployUpdatePhase, sizeof(ideployUpdatePhase), "%s", phase);
    snprintf(ideployUpdateDetail, sizeof(ideployUpdateDetail), "%s", detail.UTF8String ?: "");
}

@interface IDUpdateObserver : NSObject
@end

@implementation IDUpdateObserver
- (void)updater:(id)updater didFindValidUpdate:(id)item {
    ideploy_set_update_progress("available", @"发现可用更新，等待下载安装");
}
- (void)updaterDidNotFindUpdate:(id)updater error:(NSError *)error {
    ideploy_set_update_progress("idle", @"");
}
- (void)updater:(id)updater willDownloadUpdate:(id)item withRequest:(id)request {
    ideploy_set_update_progress("downloading", @"正在下载更新包；Sparkle 窗口显示下载百分比");
}
- (void)updater:(id)updater didDownloadUpdate:(id)item {
    ideploy_set_update_progress("verifying", @"下载完成，正在验证更新包");
}
- (void)updater:(id)updater failedToDownloadUpdate:(id)item error:(NSError *)error {
    ideploy_set_update_progress("failed", error.localizedDescription);
}
- (void)userDidCancelDownload:(id)updater {
    ideploy_set_update_progress("available", @"下载已取消，可重新点击下载安装");
}
- (void)updater:(id)updater willExtractUpdate:(id)item {
    ideploy_set_update_progress("extracting", @"正在解压并验证更新包");
}
- (void)updater:(id)updater didExtractUpdate:(id)item {
    ideploy_set_update_progress("ready", @"更新已准备就绪，等待安装或退出应用");
}
- (BOOL)updater:(id)updater willInstallUpdateOnQuit:(id)item immediateInstallationBlock:(id)handler {
    ideploy_set_update_progress("ready", @"更新已下载，将在退出应用时安装");
    return NO;
}
- (void)updater:(id)updater willInstallUpdate:(id)item {
    ideploy_set_update_progress("installing", @"正在安装更新，应用即将重新启动");
}
- (void)updater:(id)updater didAbortWithError:(NSError *)error {
    if (error.code == 1001 || error.code == 4007) {
        ideploy_set_update_progress("idle", @"");
    } else {
        ideploy_set_update_progress("failed", error.localizedDescription);
    }
}
- (void)updater:(id)updater didFinishUpdateCycleForUpdateCheck:(NSInteger)updateCheck error:(NSError *)error {
    if (strcmp(ideployUpdatePhase, "checking") == 0) {
        ideploy_set_update_progress(error == nil || error.code == 1001 ? "idle" : "failed",
                                    error == nil || error.code == 1001 ? @"" : error.localizedDescription);
    }
}
@end

// Retain the controller for the lifetime of the process. Sparkle's updater and
// UI must be created on the macOS main thread after Wails starts its run loop.
static id ideployUpdaterController;
static IDUpdateObserver *ideployUpdateObserver;

static void ideploy_on_main_thread_sync(dispatch_block_t block) {
    if ([NSThread isMainThread]) {
        block();
    } else {
        dispatch_sync(dispatch_get_main_queue(), block);
    }
}

static BOOL ideploy_saved_automatic_update_checks(void) {
    id saved = [[NSUserDefaults standardUserDefaults] objectForKey:@"SUEnableAutomaticChecks"];
    if (saved != nil) {
        return [saved boolValue];
    }
    id bundled = [[NSBundle mainBundle] objectForInfoDictionaryKey:@"SUEnableAutomaticChecks"];
    return bundled == nil ? YES : [bundled boolValue];
}

void ideploy_start_app_updater(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            NSBundle *app = [NSBundle mainBundle];
            NSDictionary *info = [app infoDictionary];
            if (![info[@"SUFeedURL"] isKindOfClass:[NSString class]] ||
                ![info[@"SUPublicEDKey"] isKindOfClass:[NSString class]]) {
                return; // Development and unsigned local builds.
            }

            NSString *path = [[app privateFrameworksPath] stringByAppendingPathComponent:@"Sparkle.framework"];
            NSBundle *sparkle = [NSBundle bundleWithPath:path];
            if (sparkle == nil || ![sparkle load]) {
                NSLog(@"iDeploy updater: unable to load Sparkle.framework at %@", path);
                return;
            }
            Class controllerClass = NSClassFromString(@"SPUStandardUpdaterController");
            if (controllerClass == Nil) {
                NSLog(@"iDeploy updater: Sparkle controller is unavailable");
                return;
            }
            ideployUpdateObserver = [[IDUpdateObserver alloc] init];
            ideployUpdaterController = [[controllerClass alloc] initWithStartingUpdater:YES
                                                                      updaterDelegate:ideployUpdateObserver
                                                                    userDriverDelegate:nil];
            IDSparkleUpdater *updater = [(IDSparkleController *)ideployUpdaterController updater];
            // Sparkle normally checks on its schedule. On a cold launch, also
            // check immediately when automatic checks are allowed by the user.
            if ([updater automaticallyChecksForUpdates]) {
                ideploy_set_update_progress("checking", @"正在检查更新");
                [updater checkForUpdatesInBackground];
            }
        }
    });
}

int ideploy_check_for_updates(void) {
    __block int available = 0;
    ideploy_on_main_thread_sync(^{
        IDSparkleController *controller = (IDSparkleController *)ideployUpdaterController;
        if (controller != nil) {
            if (strcmp(ideployUpdatePhase, "idle") == 0 || strcmp(ideployUpdatePhase, "available") == 0 || strcmp(ideployUpdatePhase, "failed") == 0) {
                ideploy_set_update_progress("checking", @"正在启动 Sparkle 更新器");
            }
            [controller checkForUpdates:nil];
            available = 1;
        }
    });
    return available;
}

char *ideploy_app_update_progress_json(void) {
    __block char *result = NULL;
    ideploy_on_main_thread_sync(^{
        @autoreleasepool {
            NSDictionary *progress = @{
                @"phase": [NSString stringWithUTF8String:ideployUpdatePhase],
                @"detail": [NSString stringWithUTF8String:ideployUpdateDetail]
            };
            NSData *data = [NSJSONSerialization dataWithJSONObject:progress options:0 error:nil];
            if (data != nil) {
                result = strndup(data.bytes, data.length);
            }
        }
    });
    return result;
}

int ideploy_automatic_update_checks(void) {
    __block BOOL enabled = NO;
    ideploy_on_main_thread_sync(^{
        IDSparkleUpdater *updater = [(IDSparkleController *)ideployUpdaterController updater];
        enabled = updater == nil ? ideploy_saved_automatic_update_checks() : [updater automaticallyChecksForUpdates];
    });
    return enabled;
}

void ideploy_set_automatic_update_checks(int enabled) {
    ideploy_on_main_thread_sync(^{
        IDSparkleUpdater *updater = [(IDSparkleController *)ideployUpdaterController updater];
        if (updater == nil) {
            [[NSUserDefaults standardUserDefaults] setBool:(enabled != 0) forKey:@"SUEnableAutomaticChecks"];
        } else {
            [updater setAutomaticallyChecksForUpdates:(enabled != 0)];
        }
    });
}
