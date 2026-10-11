#import <Foundation/Foundation.h>
#import <dispatch/dispatch.h>

// Keep the bridge independent of Sparkle headers so normal Go builds do not
// need the release framework installed on the development machine.
@interface IDSparkleUpdater : NSObject
- (BOOL)automaticallyChecksForUpdates;
- (void)checkForUpdatesInBackground;
@end

@interface IDSparkleController : NSObject
- (instancetype)initWithStartingUpdater:(BOOL)start
                         updaterDelegate:(id)delegate
                       userDriverDelegate:(id)driver;
- (IDSparkleUpdater *)updater;
@end

// Retain the controller for the lifetime of the process. Sparkle's updater and
// UI must be created on the macOS main thread after Wails starts its run loop.
static id ideployUpdaterController;

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
            ideployUpdaterController = [[controllerClass alloc] initWithStartingUpdater:YES
                                                                      updaterDelegate:nil
                                                                    userDriverDelegate:nil];
            IDSparkleUpdater *updater = [(IDSparkleController *)ideployUpdaterController updater];
            // Sparkle normally checks on its schedule. On a cold launch, also
            // check immediately when automatic checks are allowed by the user.
            if ([updater automaticallyChecksForUpdates]) {
                [updater checkForUpdatesInBackground];
            }
        }
    });
}
