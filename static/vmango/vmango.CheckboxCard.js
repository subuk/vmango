(function(exports){
    exports.Vmango = exports.Vmango || {};
    exports.Vmango.CheckboxCards = function(selector){
        var $rootEl = $(selector),
            multiple = $rootEl.attr('data-CheckboxCards-Multiple') === "true",
            autoselect = $rootEl.attr('data-CheckboxCards-AutoSelect') === "true",
            $cardEls = $('.JS-CheckboxCard', selector),
            defaultColor = $cardEls.first().css('border-color'),
            $checkboxEls = $cardEls.find('input[type=checkbox]');

        $checkboxEls.hide();

        $cardEls.each(function(idx, el){
            var $el = $(el),
                $icon = $el.find('.icon');

            if ($el.find('input[type=checkbox]').prop('checked')){
                $el.css({'border-color': 'green'})
                $icon.show();
            }
        });

        $checkboxEls.on('change', function($event){
            var $el = $(this),
                $cardEl = $el.closest('.JS-CheckboxCard'),
                $icon = $cardEl.find('.icon');

            if ($el.prop('checked')){
                $cardEl.css({'border-color': 'green'});
                $icon.show();
            } else {
                $cardEl.css({'border-color': defaultColor});
                $icon.hide();
            }
        });

        $cardEls.on('click', function($event){
            var newState, $allChecked, $dependEls, dependsShowSelector,
                $el = $(this),
                $checkboxEl = $el.find('input[type=checkbox]'),
                dependSelector = $el.attr('data-CheckboxCards-Depends');


            $event.preventDefault();
            newState = !$checkboxEl.prop("checked");
            if (!multiple && !newState) {
                return
            }
            if (!multiple){
                $allChecked = $cardEls.find('input[type=checkbox]:checked');
                $allChecked.prop('checked', false);
                $allChecked.trigger('change');
            }
            $checkboxEl.prop("checked", newState);
            $checkboxEl.trigger('change');

            if (dependSelector != "") {
                $dependEls = $(dependSelector).find('.JS-CheckboxCard'),
                dependsShowSelector = $el.attr('data-CheckboxCards-Depends-ShowOnly');
                $dependEls.hide();
                $dependEls.filter(dependsShowSelector).show();
            }
        });

        if (autoselect && $checkboxEls.find(":selected").length <= 0){
            $cardEls.first().trigger('click');
        }

    }
})(window);
